package docscan

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"database_scan/internal/db"
	"database_scan/internal/detector"
	"database_scan/internal/scanner"
)

func ScanElasticsearch(ctx context.Context, cfg Config) (db.ServerInfo, scanner.Result, error) {
	client, err := newElasticClient(cfg)
	if err != nil {
		return db.ServerInfo{}, scanner.Result{}, err
	}
	info, err := elasticInfo(ctx, client, cfg)
	if err != nil {
		return info, scanner.Result{}, err
	}
	indices, err := elasticIndices(ctx, client, cfg)
	if err != nil {
		return info, scanner.Result{}, err
	}
	result := scanner.Result{}
	for _, index := range indices {
		if err := ctx.Err(); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("scan interrupted: %v", err))
			return info, result, nil
		}
		progressf(cfg.Progress, "扫描 Elasticsearch index %s...\n", index)
		table, summaries, err := scanElasticIndex(ctx, client, cfg, index)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", index, err))
			continue
		}
		if len(table.Fields) == 0 {
			continue
		}
		result.Tables = append(result.Tables, table)
		result.Summaries = append(result.Summaries, summaries...)
	}
	return info, result, nil
}

func TestElasticsearchConnection(ctx context.Context, cfg Config) (db.ServerInfo, error) {
	client, err := newElasticClient(cfg)
	if err != nil {
		return db.ServerInfo{}, err
	}
	return elasticInfo(ctx, client, cfg)
}

type elasticClient struct {
	base   string
	user   string
	pass   string
	client *http.Client
}

func newElasticClient(cfg Config) (*elasticClient, error) {
	if cfg.Port == 0 {
		cfg.Port = 9200
	}
	base := (&url.URL{Scheme: "http", Host: targetLabel(cfg)}).String()
	transport := &http.Transport{}
	if cfg.Proxy != "" {
		proxyURL, err := url.Parse(cfg.Proxy)
		if err != nil {
			return nil, err
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	return &elasticClient{
		base:   strings.TrimRight(base, "/"),
		user:   cfg.User,
		pass:   cfg.Password,
		client: &http.Client{Timeout: cfg.Timeout, Transport: transport},
	}, nil
}

func (c *elasticClient) getJSON(ctx context.Context, path string, out any) error {
	return c.doJSON(ctx, http.MethodGet, path, nil, out)
}

func (c *elasticClient) postJSON(ctx context.Context, path string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return c.doJSON(ctx, http.MethodPost, path, bytes.NewReader(data), out)
}

func (c *elasticClient) doJSON(ctx context.Context, method, path string, body io.Reader, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.user != "" || c.pass != "" {
		req.SetBasicAuth(c.user, c.pass)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("%s %s: %s", method, path, strings.TrimSpace(string(data)))
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func elasticInfo(ctx context.Context, client *elasticClient, cfg Config) (db.ServerInfo, error) {
	info := db.ServerInfo{Host: cfg.Host, Port: cfg.Port, DBType: "Elasticsearch", CurrentDB: cfg.Database, CurrentUser: cfg.User, Proxy: cfg.Proxy, IncludeSystem: cfg.IncludeSystem, Environment: map[string]string{}}
	var root struct {
		Name        string `json:"name"`
		ClusterName string `json:"cluster_name"`
		Version     struct {
			Number string `json:"number"`
		} `json:"version"`
		Tagline string `json:"tagline"`
	}
	if err := client.getJSON(ctx, "/", &root); err != nil {
		return info, fmt.Errorf("elasticsearch info: %w", err)
	}
	info.Version = root.Version.Number
	info.Environment["name"] = root.Name
	info.Environment["cluster_name"] = root.ClusterName
	info.Environment["tagline"] = root.Tagline
	withResolvedAddr(&info, cfg.Host)
	return info, nil
}

func elasticIndices(ctx context.Context, client *elasticClient, cfg Config) ([]string, error) {
	if wanted := splitList(cfg.Database); len(wanted) > 0 {
		return wanted, nil
	}
	var rows []struct {
		Index string `json:"index"`
	}
	if err := client.getJSON(ctx, "/_cat/indices?format=json&h=index", &rows); err != nil {
		return nil, err
	}
	var indices []string
	for _, row := range rows {
		if row.Index == "" {
			continue
		}
		if !cfg.IncludeSystem && strings.HasPrefix(row.Index, ".") {
			continue
		}
		indices = append(indices, row.Index)
	}
	return indices, nil
}

func scanElasticIndex(ctx context.Context, client *elasticClient, cfg Config, index string) (scanner.TableResult, []scanner.Summary, error) {
	limit := cfg.Limit
	if limit <= 0 {
		limit = 15
	}
	var search struct {
		Hits struct {
			Total any `json:"total"`
			Hits  []struct {
				ID     string         `json:"_id"`
				Source map[string]any `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	body := map[string]any{"query": map[string]any{"match_all": map[string]any{}}, "size": limit}
	if err := client.postJSON(ctx, "/"+url.PathEscape(index)+"/_search", body, &search); err != nil {
		return scanner.TableResult{}, nil, err
	}
	table := scanner.TableResult{
		Database: index,
		Schema:   "elasticsearch",
		Name:     index,
		Total:    elasticTotal(search.Hits.Total),
		Columns:  []string{"Target", "Index", "Document", "Path/Field", "Value", "命中类型", "敏感级别", "判断依据"},
	}
	fields := map[string]*scanner.FieldResult{}
	summaryResult := scanner.Result{}
	for _, hit := range search.Hits.Hits {
		var entries []valueEntry
		flattenAny("", hit.Source, cfg.TextEncoding, &entries)
		for _, entry := range entries {
			kinds, reason := combineKinds(cfg.Level, entry.Path, entry.Value)
			if len(kinds) == 0 {
				continue
			}
			addField(fields, entry.Path, kinds)
			addSummary(&summaryResult, table, entry.Path, kinds)
			table.Rows = append(table.Rows, scanner.RowSample{Values: map[string]string{
				"Target":     targetLabel(cfg),
				"Index":      index,
				"Document":   hit.ID,
				"Path/Field": entry.Path,
				"Value":      maskValue(kinds, entry.Value, cfg.Mask),
				"命中类型":       scanner.KindLabel(kinds),
				"敏感级别":       detector.LevelLabel(highestLevel(kinds)),
				"判断依据":       reason,
			}})
		}
	}
	table.Fields = fieldSlice(fields)
	return table, summaryResult.Summaries, nil
}

func elasticTotal(value any) int64 {
	switch v := value.(type) {
	case float64:
		return int64(v)
	case map[string]any:
		if n, ok := v["value"].(float64); ok {
			return int64(n)
		}
	case string:
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	}
	return 0
}
