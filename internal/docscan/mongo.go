package docscan

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"database_scan/internal/db"
	"database_scan/internal/detector"
	iproxy "database_scan/internal/proxy"
	"database_scan/internal/scanner"
	"database_scan/internal/textfix"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

func ScanMongo(ctx context.Context, cfg Config) (db.ServerInfo, scanner.Result, error) {
	client, err := openMongo(ctx, cfg)
	if err != nil {
		return db.ServerInfo{}, scanner.Result{}, err
	}
	defer client.Disconnect(context.Background())
	info, err := mongoInfo(ctx, client, cfg)
	if err != nil {
		return info, scanner.Result{}, err
	}
	databases, err := mongoDatabases(ctx, client, cfg)
	if err != nil {
		return info, scanner.Result{}, err
	}
	result := scanner.Result{}
	for _, database := range databases {
		if err := ctx.Err(); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("scan interrupted: %v", err))
			return info, result, nil
		}
		collections, err := client.Database(database).ListCollectionNames(ctx, bson.D{})
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: list collections: %v", database, err))
			continue
		}
		for _, collection := range collections {
			progressf(cfg.Progress, "扫描 MongoDB %s.%s...\n", database, collection)
			table, summaries, err := scanMongoCollection(ctx, client.Database(database).Collection(collection), cfg, database, collection)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("%s.%s: %v", database, collection, err))
				continue
			}
			if len(table.Fields) == 0 {
				continue
			}
			result.Tables = append(result.Tables, table)
			result.Summaries = append(result.Summaries, summaries...)
		}
	}
	return info, result, nil
}

func TestMongoConnection(ctx context.Context, cfg Config) (db.ServerInfo, error) {
	client, err := openMongo(ctx, cfg)
	if err != nil {
		return db.ServerInfo{}, err
	}
	defer client.Disconnect(context.Background())
	return mongoInfo(ctx, client, cfg)
}

func openMongo(ctx context.Context, cfg Config) (*mongo.Client, error) {
	if cfg.Port == 0 {
		cfg.Port = 27017
	}
	uri := (&url.URL{Scheme: "mongodb", Host: net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))}).String()
	opts := options.Client().ApplyURI(uri).SetConnectTimeout(cfg.Timeout).SetServerSelectionTimeout(cfg.Timeout)
	if cfg.User != "" {
		authDB := strings.TrimSpace(cfg.AuthDatabase)
		if authDB == "" {
			authDB = strings.TrimSpace(cfg.Database)
		}
		if authDB == "" {
			authDB = "admin"
		}
		opts.SetAuth(options.Credential{AuthSource: authDB, Username: cfg.User, Password: cfg.Password})
	}
	if cfg.Proxy != "" {
		dialer, err := iproxy.FromURL(cfg.Proxy, cfg.Timeout)
		if err != nil {
			return nil, err
		}
		opts.SetDialer(mongoDialer{dialer: dialer})
	}
	client, err := mongo.Connect(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("connect mongodb: %w", err)
	}
	if err := client.Ping(ctx, readpref.Primary()); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, fmt.Errorf("ping mongodb: %w", err)
	}
	return client, nil
}

type mongoDialer struct {
	dialer db.ContextDialer
}

func (d mongoDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return d.dialer.DialContext(ctx, network, address)
}

func mongoInfo(ctx context.Context, client *mongo.Client, cfg Config) (db.ServerInfo, error) {
	info := db.ServerInfo{Host: cfg.Host, Port: cfg.Port, DBType: "MongoDB", CurrentDB: cfg.Database, CurrentUser: cfg.User, Proxy: cfg.Proxy, IncludeSystem: cfg.IncludeSystem, Environment: map[string]string{}}
	var build bson.M
	if err := client.Database("admin").RunCommand(ctx, bson.D{{Key: "buildInfo", Value: 1}}).Decode(&build); err != nil {
		return info, fmt.Errorf("mongodb buildInfo: %w", err)
	}
	info.Version = fmt.Sprint(build["version"])
	if git, ok := build["gitVersion"]; ok {
		info.Environment["git_version"] = fmt.Sprint(git)
	}
	var serverStatus bson.M
	if err := client.Database("admin").RunCommand(ctx, bson.D{{Key: "serverStatus", Value: 1}}).Decode(&serverStatus); err == nil {
		if host, ok := serverStatus["host"]; ok {
			info.Environment["host"] = fmt.Sprint(host)
		}
		if localTime, ok := serverStatus["localTime"]; ok {
			info.ServerTime = fmt.Sprint(localTime)
		}
	}
	withResolvedAddr(&info, cfg.Host)
	return info, nil
}

func mongoDatabases(ctx context.Context, client *mongo.Client, cfg Config) ([]string, error) {
	if wanted := splitList(cfg.Database); len(wanted) > 0 {
		return wanted, nil
	}
	names, err := client.ListDatabaseNames(ctx, bson.D{})
	if err != nil {
		return nil, err
	}
	if cfg.IncludeSystem {
		return names, nil
	}
	out := names[:0]
	for _, name := range names {
		switch strings.ToLower(name) {
		case "admin", "config", "local":
		default:
			out = append(out, name)
		}
	}
	return out, nil
}

func scanMongoCollection(ctx context.Context, coll *mongo.Collection, cfg Config, database, collection string) (scanner.TableResult, []scanner.Summary, error) {
	limit := int64(cfg.Limit)
	if limit <= 0 {
		limit = 15
	}
	findOpts := options.Find().SetLimit(limit)
	cursor, err := coll.Find(ctx, bson.D{}, findOpts)
	if err != nil {
		return scanner.TableResult{}, nil, err
	}
	defer cursor.Close(ctx)
	total, _ := coll.EstimatedDocumentCount(ctx)
	table := scanner.TableResult{
		Database: database,
		Schema:   "mongodb",
		Name:     collection,
		Total:    total,
		Columns:  []string{"Target", "Database", "Collection", "Path/Field", "Value", "命中类型", "敏感级别", "判断依据"},
	}
	fields := map[string]*scanner.FieldResult{}
	summaryResult := scanner.Result{}
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			return table, summaryResult.Summaries, err
		}
		for _, entry := range flattenBSON(doc, cfg.TextEncoding) {
			kinds, reason := combineKinds(cfg.Level, entry.Path, entry.Value)
			if len(kinds) == 0 {
				continue
			}
			addField(fields, entry.Path, kinds)
			addSummary(&summaryResult, table, entry.Path, kinds)
			table.Rows = append(table.Rows, scanner.RowSample{Values: map[string]string{
				"Target":     targetLabel(cfg),
				"Database":   database,
				"Collection": collection,
				"Path/Field": entry.Path,
				"Value":      maskValue(kinds, entry.Value, cfg.Mask),
				"命中类型":       scanner.KindLabel(kinds),
				"敏感级别":       detectorLevelLabel(kinds),
				"判断依据":       reason,
			}})
		}
	}
	table.Fields = fieldSlice(fields)
	return table, summaryResult.Summaries, cursor.Err()
}

func flattenBSON(value any, textEncoding string) []valueEntry {
	var generic any
	data, err := json.Marshal(value)
	if err == nil {
		_ = json.Unmarshal(data, &generic)
	}
	if generic == nil {
		generic = value
	}
	var out []valueEntry
	flattenAny("", generic, textEncoding, &out)
	return out
}

func flattenAny(path string, value any, textEncoding string, out *[]valueEntry) {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			next := key
			if path != "" {
				next = path + "." + key
			}
			flattenAny(next, child, textEncoding, out)
		}
	case []any:
		for i, child := range v {
			next := fmt.Sprintf("%s[%d]", path, i)
			flattenAny(next, child, textEncoding, out)
		}
	default:
		if path == "" || value == nil {
			return
		}
		text := textfix.RepairString(fmt.Sprint(value), textEncoding)
		if strings.TrimSpace(text) == "" {
			return
		}
		*out = append(*out, valueEntry{Path: path, Value: text})
	}
}

func detectorLevelLabel(kinds []detector.Kind) string {
	return detector.LevelLabel(highestLevel(kinds))
}
