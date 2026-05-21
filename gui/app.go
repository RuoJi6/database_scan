package main

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"sync"
	"time"

	coreapp "database_scan/internal/app"
	"database_scan/internal/scanner"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx context.Context

	mu     sync.Mutex
	cancel context.CancelFunc
	state  coreapp.ScanJobState
}

func NewApp() *App {
	return &App{state: coreapp.ScanJobState{Status: "idle", Request: coreapp.DefaultScanRequest()}}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

func (a *App) GetDefaults() coreapp.ScanRequest {
	return coreapp.DefaultScanRequest()
}

func (a *App) GetSupportedDatabaseTypes() []string {
	return coreapp.SupportedDatabaseTypes()
}

func (a *App) StartScan(req coreapp.ScanRequest) (coreapp.ScanJobState, error) {
	a.mu.Lock()
	if a.cancel != nil && a.state.Status == "running" {
		a.mu.Unlock()
		return coreapp.ScanJobState{}, fmt.Errorf("已有扫描任务正在运行")
	}
	jobID := uuid.NewString()
	ctx, cancel := context.WithCancel(a.ctx)
	a.cancel = cancel
	a.state = coreapp.ScanJobState{
		JobID:     jobID,
		Status:    "running",
		Progress:  1,
		Message:   "扫描任务已启动",
		Request:   req,
		StartedAt: time.Now().Format(time.RFC3339),
		Logs: []coreapp.LogEntry{{
			Time:    time.Now().Format("15:04:05"),
			Level:   "info",
			Message: "GUI 已提交扫描任务",
		}},
	}
	startState := a.state
	a.mu.Unlock()

	go a.runScan(ctx, jobID, req)
	return startState, nil
}

func (a *App) runScan(ctx context.Context, jobID string, req coreapp.ScanRequest) {
	state, err := coreapp.RunScan(ctx, req, coreapp.ServiceHooks{
		OnLog: func(entry coreapp.LogEntry) {
			a.mu.Lock()
			defer a.mu.Unlock()
			if a.state.JobID != jobID {
				return
			}
			a.state.Logs = append(a.state.Logs, entry)
			if len(a.state.Logs) > 220 {
				a.state.Logs = a.state.Logs[len(a.state.Logs)-220:]
			}
			a.state.Message = entry.Message
		},
		OnTable: func(table scanner.TableResult) {
			a.mu.Lock()
			defer a.mu.Unlock()
			if a.state.JobID != jobID {
				return
			}
			a.state.Result.Tables = append(a.state.Result.Tables, table)
			a.state.Progress = clampProgress(a.state.Progress + 3)
		},
		OnTarget: func(index int, total int, label string) {
			a.mu.Lock()
			defer a.mu.Unlock()
			if a.state.JobID != jobID {
				return
			}
			a.state.TargetLabel = label
			a.state.Progress = clampProgress(int(float64(index-1) / float64(total) * 100))
		},
	})

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state.JobID != jobID {
		return
	}
	if ctx.Err() != nil && state.Status == "running" {
		state.Status = "stopped"
		state.Message = "扫描已停止"
		state.FinishedAt = time.Now().Format(time.RFC3339)
	}
	state.JobID = jobID
	if len(state.Logs) == 0 {
		state.Logs = a.state.Logs
	}
	if err != nil && state.Status == "" {
		state.Status = "failed"
		state.Message = err.Error()
		state.Errors = append(state.Errors, err.Error())
		state.FinishedAt = time.Now().Format(time.RFC3339)
	}
	a.state = state
	a.cancel = nil
}

func (a *App) StopScan(jobID string) (coreapp.ScanJobState, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if jobID != "" && a.state.JobID != jobID {
		return a.state, fmt.Errorf("任务不存在或已结束")
	}
	if a.cancel != nil {
		a.cancel()
	}
	a.state.Status = "stopped"
	a.state.Message = "已发送停止信号，等待当前查询退出"
	a.state.Logs = append(a.state.Logs, coreapp.LogEntry{Time: time.Now().Format("15:04:05"), Level: "warn", Message: "用户停止扫描"})
	return a.state, nil
}

func (a *App) GetScanState(jobID string) coreapp.ScanJobState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.state
}

func (a *App) RunCustomSQL(req coreapp.ScanRequest) (coreapp.CustomSQLResult, error) {
	return coreapp.RunCustomSQL(a.ctx, req)
}

func (a *App) ParseFscanFile(path string) (coreapp.FscanPreview, error) {
	return coreapp.ParseFscanPreview(path)
}

func (a *App) ParseFscanText(text string) (coreapp.FscanPreview, error) {
	return coreapp.ParseFscanTextPreview(text)
}

func (a *App) ChooseFscanFile() (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择 fscan 结果文件",
		Filters: []runtime.FileFilter{
			{DisplayName: "Text", Pattern: "*.txt;*.log;*.out"},
			{DisplayName: "All Files", Pattern: "*"},
		},
	})
}

func (a *App) ChooseOutputPath() (string, error) {
	return runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "保存 Excel 报告",
		DefaultFilename: "database_scan_report.xlsx",
		Filters: []runtime.FileFilter{
			{DisplayName: "Excel Workbook", Pattern: "*.xlsx"},
		},
	})
}

func (a *App) OpenOutputFolder(path string) error {
	if path == "" {
		return nil
	}
	dir := filepath.Dir(path)
	runtime.BrowserOpenURL(a.ctx, (&url.URL{Scheme: "file", Path: dir}).String())
	return nil
}

func clampProgress(value int) int {
	if value < 0 {
		return 0
	}
	if value > 99 {
		return 99
	}
	return value
}
