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

	vault *taskVault

	mu           sync.Mutex
	runtimes     map[string]context.CancelFunc
	legacyCancel context.CancelFunc
	legacyState  coreapp.ScanJobState
}

func NewApp() *App {
	vault, err := newTaskVault()
	if err != nil {
		vault = &taskVault{path: filepath.Join(".", "database_scan", "database_scan.db")}
	}
	return &App{
		vault:       vault,
		runtimes:    map[string]context.CancelFunc{},
		legacyState: coreapp.ScanJobState{Status: "idle", Request: coreapp.DefaultScanRequest()},
	}
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

func (a *App) GetVaultStatus() VaultStatus {
	if a.vault == nil {
		return VaultStatus{}
	}
	return a.vault.status()
}

func (a *App) SetupVault(password string) (VaultStatus, error) {
	return a.vault.setup(password)
}

func (a *App) UnlockVault(password string) (VaultStatus, error) {
	return a.vault.unlock(password)
}

func (a *App) ResetVault() (VaultStatus, error) {
	a.mu.Lock()
	for _, cancel := range a.runtimes {
		cancel()
	}
	a.runtimes = map[string]context.CancelFunc{}
	a.mu.Unlock()
	return a.vault.reset()
}

func (a *App) ListTasks() ([]GUITask, error) {
	return a.vault.listTasks()
}

func (a *App) CreateTask(req CreateTaskRequest) (GUITask, error) {
	return a.vault.createTask(req)
}

func (a *App) UpdateTask(req UpdateTaskRequest) (GUITask, error) {
	return a.vault.updateTask(req)
}

func (a *App) DeleteTask(id string) error {
	a.mu.Lock()
	if cancel := a.runtimes[id]; cancel != nil {
		cancel()
		delete(a.runtimes, id)
	}
	a.mu.Unlock()
	return a.vault.deleteTask(id)
}

func (a *App) ChooseBackupExportPath() (string, error) {
	return runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "导出数据备份",
		DefaultFilename: "database_scan_backup.dbsbak",
		Filters: []runtime.FileFilter{
			{DisplayName: "Database Scan Backup", Pattern: "*.dbsbak"},
		},
	})
}

func (a *App) ChooseBackupImportFile() (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "导入数据备份",
		Filters: []runtime.FileFilter{
			{DisplayName: "Database Scan Backup", Pattern: "*.dbsbak"},
			{DisplayName: "All Files", Pattern: "*"},
		},
	})
}

func (a *App) ExportDataBackup(req BackupExportRequest) (BackupResult, error) {
	return a.vault.exportBackup(req)
}

func (a *App) ImportDataBackup(req BackupImportRequest) (BackupResult, error) {
	a.mu.Lock()
	running := len(a.runtimes)
	a.mu.Unlock()
	if running > 0 {
		return BackupResult{}, fmt.Errorf("请先停止或等待运行中的任务结束，再导入备份")
	}
	return a.vault.importBackup(req)
}

func (a *App) GetTask(id string) (GUITask, error) {
	return a.vault.getTask(id)
}

func (a *App) StartTask(id string) (GUITask, error) {
	task, err := a.vault.getTask(id)
	if err != nil {
		return GUITask{}, err
	}
	a.mu.Lock()
	if _, running := a.runtimes[id]; running {
		a.mu.Unlock()
		return GUITask{}, fmt.Errorf("任务 %s 已在运行", task.Name)
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.runtimes[id] = cancel
	a.mu.Unlock()

	now := time.Now().Format(time.RFC3339)
	task.Status = "running"
	task.Progress = 1
	task.Message = "任务已启动"
	task.StartedAt = now
	task.FinishedAt = ""
	task.State = coreapp.ScanJobState{
		JobID:     id,
		Status:    "running",
		Progress:  1,
		Message:   "任务已启动",
		Request:   task.Request,
		StartedAt: now,
		Logs: []coreapp.LogEntry{{
			Time:    time.Now().Format("15:04:05"),
			Level:   "info",
			Message: "任务已提交扫描引擎",
		}},
	}
	task, err = a.vault.replaceTask(task)
	if err != nil {
		a.mu.Lock()
		delete(a.runtimes, id)
		a.mu.Unlock()
		cancel()
		return GUITask{}, err
	}

	go a.runTask(ctx, id)
	return task, nil
}

func (a *App) runTask(ctx context.Context, taskID string) {
	task, err := a.vault.getTask(taskID)
	if err != nil {
		a.clearRuntime(taskID)
		return
	}
	state, runErr := coreapp.RunScan(ctx, task.Request, coreapp.ServiceHooks{
		OnLog: func(entry coreapp.LogEntry) {
			a.updateRunningTask(taskID, func(task *GUITask) {
				task.State.Logs = append(task.State.Logs, entry)
				if len(task.State.Logs) > 240 {
					task.State.Logs = task.State.Logs[len(task.State.Logs)-240:]
				}
				task.State.Message = entry.Message
				task.Message = entry.Message
			})
		},
		OnTable: func(table scanner.TableResult) {
			a.updateRunningTask(taskID, func(task *GUITask) {
				task.State.Result.Tables = append(task.State.Result.Tables, table)
				task.State.Progress = clampProgress(task.State.Progress + 3)
				task.Progress = task.State.Progress
			})
		},
		OnTarget: func(index int, total int, label string) {
			a.updateRunningTask(taskID, func(task *GUITask) {
				task.State.TargetLabel = label
				task.TargetLabel = label
				if total > 0 {
					task.State.Progress = clampProgress(int(float64(index-1) / float64(total) * 100))
					task.Progress = task.State.Progress
				}
			})
		},
	})

	a.updateRunningTask(taskID, func(task *GUITask) {
		if ctx.Err() != nil && state.Status == "running" {
			state.Status = "stopped"
			state.Message = "扫描已停止"
			state.FinishedAt = time.Now().Format(time.RFC3339)
		}
		state.JobID = taskID
		if len(state.Logs) == 0 {
			state.Logs = task.State.Logs
		}
		if runErr != nil && state.Status == "" {
			state.Status = "failed"
			state.Message = runErr.Error()
			state.Errors = append(state.Errors, runErr.Error())
			state.FinishedAt = time.Now().Format(time.RFC3339)
		}
		task.State = state
		task.Status = state.Status
		task.Progress = state.Progress
		task.Message = state.Message
		task.TargetLabel = state.TargetLabel
		task.StartedAt = state.StartedAt
		task.FinishedAt = state.FinishedAt
	})
	a.clearRuntime(taskID)
}

func (a *App) updateRunningTask(id string, mutate func(*GUITask)) {
	task, err := a.vault.getTask(id)
	if err != nil {
		return
	}
	mutate(&task)
	_, _ = a.vault.replaceTask(task)
}

func (a *App) clearRuntime(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.runtimes, id)
}

func (a *App) StopTask(id string) (GUITask, error) {
	a.mu.Lock()
	cancel := a.runtimes[id]
	if cancel != nil {
		cancel()
	}
	a.mu.Unlock()
	task, err := a.vault.getTask(id)
	if err != nil {
		return GUITask{}, err
	}
	task.Status = "stopped"
	task.Progress = task.State.Progress
	task.Message = "已发送停止信号，等待当前查询退出"
	task.State.Status = "stopped"
	task.State.Message = task.Message
	task.State.Logs = append(task.State.Logs, coreapp.LogEntry{Time: time.Now().Format("15:04:05"), Level: "warn", Message: "用户停止扫描"})
	return a.vault.replaceTask(task)
}

func (a *App) GetTaskState(id string) (coreapp.ScanJobState, error) {
	task, err := a.vault.getTask(id)
	if err != nil {
		return coreapp.ScanJobState{}, err
	}
	return task.State, nil
}

func (a *App) RunTaskSQL(id string) (GUITask, error) {
	task, err := a.vault.getTask(id)
	if err != nil {
		return GUITask{}, err
	}
	result, err := coreapp.RunCustomSQL(a.ctx, task.Request)
	if err != nil {
		task.Status = "failed"
		task.Message = err.Error()
		task.State.Status = "failed"
		task.State.Message = err.Error()
		task.State.Errors = append(task.State.Errors, err.Error())
		task.FinishedAt = time.Now().Format(time.RFC3339)
		task.State.FinishedAt = task.FinishedAt
		_, _ = a.vault.replaceTask(task)
		return GUITask{}, err
	}
	now := time.Now().Format(time.RFC3339)
	task.Status = "completed"
	task.Progress = 100
	task.Message = "SQL 执行完成"
	task.State = coreapp.ScanJobState{
		JobID:      id,
		Status:     "completed",
		Progress:   100,
		Message:    task.Message,
		Request:    task.Request,
		SQLResult:  &result,
		StartedAt:  now,
		FinishedAt: now,
	}
	task.StartedAt = now
	task.FinishedAt = now
	return a.vault.replaceTask(task)
}

func (a *App) StartScan(req coreapp.ScanRequest) (coreapp.ScanJobState, error) {
	a.mu.Lock()
	if a.legacyCancel != nil && a.legacyState.Status == "running" {
		a.mu.Unlock()
		return coreapp.ScanJobState{}, fmt.Errorf("已有扫描任务正在运行")
	}
	jobID := uuid.NewString()
	ctx, cancel := context.WithCancel(a.ctx)
	a.legacyCancel = cancel
	a.legacyState = coreapp.ScanJobState{
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
	startState := a.legacyState
	a.mu.Unlock()

	go a.runScan(ctx, jobID, req)
	return startState, nil
}

func (a *App) runScan(ctx context.Context, jobID string, req coreapp.ScanRequest) {
	state, err := coreapp.RunScan(ctx, req, coreapp.ServiceHooks{
		OnLog: func(entry coreapp.LogEntry) {
			a.mu.Lock()
			defer a.mu.Unlock()
			if a.legacyState.JobID != jobID {
				return
			}
			a.legacyState.Logs = append(a.legacyState.Logs, entry)
			if len(a.legacyState.Logs) > 220 {
				a.legacyState.Logs = a.legacyState.Logs[len(a.legacyState.Logs)-220:]
			}
			a.legacyState.Message = entry.Message
		},
		OnTable: func(table scanner.TableResult) {
			a.mu.Lock()
			defer a.mu.Unlock()
			if a.legacyState.JobID != jobID {
				return
			}
			a.legacyState.Result.Tables = append(a.legacyState.Result.Tables, table)
			a.legacyState.Progress = clampProgress(a.legacyState.Progress + 3)
		},
		OnTarget: func(index int, total int, label string) {
			a.mu.Lock()
			defer a.mu.Unlock()
			if a.legacyState.JobID != jobID {
				return
			}
			a.legacyState.TargetLabel = label
			a.legacyState.Progress = clampProgress(int(float64(index-1) / float64(total) * 100))
		},
	})

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.legacyState.JobID != jobID {
		return
	}
	if ctx.Err() != nil && state.Status == "running" {
		state.Status = "stopped"
		state.Message = "扫描已停止"
		state.FinishedAt = time.Now().Format(time.RFC3339)
	}
	state.JobID = jobID
	if len(state.Logs) == 0 {
		state.Logs = a.legacyState.Logs
	}
	if err != nil && state.Status == "" {
		state.Status = "failed"
		state.Message = err.Error()
		state.Errors = append(state.Errors, err.Error())
		state.FinishedAt = time.Now().Format(time.RFC3339)
	}
	a.legacyState = state
	a.legacyCancel = nil
}

func (a *App) StopScan(jobID string) (coreapp.ScanJobState, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if jobID != "" && a.legacyState.JobID != jobID {
		return a.legacyState, fmt.Errorf("任务不存在或已结束")
	}
	if a.legacyCancel != nil {
		a.legacyCancel()
	}
	a.legacyState.Status = "stopped"
	a.legacyState.Message = "已发送停止信号，等待当前查询退出"
	a.legacyState.Logs = append(a.legacyState.Logs, coreapp.LogEntry{Time: time.Now().Format("15:04:05"), Level: "warn", Message: "用户停止扫描"})
	return a.legacyState, nil
}

func (a *App) GetScanState(jobID string) coreapp.ScanJobState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.legacyState
}

func (a *App) RunCustomSQL(req coreapp.ScanRequest) (coreapp.CustomSQLResult, error) {
	return coreapp.RunCustomSQL(a.ctx, req)
}

func (a *App) TestConnection(req coreapp.ScanRequest) (coreapp.ConnectionTestResult, error) {
	return coreapp.TestConnection(a.ctx, req)
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
