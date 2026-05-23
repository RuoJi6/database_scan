package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	coreapp "database_scan/internal/app"
)

func TestGUIE2ETaskFlowsAgainstVM(t *testing.T) {
	if os.Getenv("DATABASE_SCAN_GUI_E2E") != "1" {
		t.Skip("set DATABASE_SCAN_GUI_E2E=1 to run VM-backed GUI task flow tests")
	}
	host := envOr("DATABASE_SCAN_E2E_HOST", "10.211.55.16")
	mysqlPort := envOrInt("DATABASE_SCAN_E2E_MYSQL_PORT", 13306)
	postgresPort := envOrInt("DATABASE_SCAN_E2E_POSTGRES_PORT", 15432)

	app := NewApp()
	app.ctx = context.Background()
	app.vault = testVault(t)

	if _, err := app.SetupVault("gui-e2e-password"); err != nil {
		t.Fatalf("SetupVault failed: %v", err)
	}

	mysqlReq := coreapp.ScanRequest{
		Type:     "mysql",
		Host:     host,
		Port:     mysqlPort,
		User:     "root",
		Password: "scanpass",
		Database: "audit_lab",
		Mode:     "field-content",
		Level:    "all",
		Limit:    5,
		Workers:  2,
		Timeout:  "10s",
		Output:   filepath.Join(t.TempDir(), "gui-single.xlsx"),
	}
	testResult, err := app.TestConnection(mysqlReq)
	if err != nil {
		t.Fatalf("TestConnection(mysql) failed: %v", err)
	}
	if !testResult.Success || testResult.Type != "mysql" {
		t.Fatalf("unexpected mysql connection result: %#v", testResult)
	}

	singleTask, err := app.CreateTask(CreateTaskRequest{
		Name:        "GUI E2E 单目标扫描",
		Description: "从新 GUI 任务 API 创建并扫描虚拟机 MySQL",
		Kind:        "single",
		Request:     mysqlReq,
	})
	if err != nil {
		t.Fatalf("CreateTask(single) failed: %v", err)
	}
	if _, err := app.StartTask(singleTask.ID); err != nil {
		t.Fatalf("StartTask(single) failed: %v", err)
	}
	singleTask = waitForTask(t, app, singleTask.ID, 45*time.Second)
	assertCompletedTask(t, singleTask)
	if len(singleTask.State.Result.Tables) == 0 {
		t.Fatalf("single task returned no scanned tables")
	}
	assertFileExists(t, mysqlReq.Output)

	sqlReq := mysqlReq
	sqlReq.Output = ""
	sqlReq.SQL = "select count(*) as table_count from information_schema.tables where table_schema = 'audit_lab'"
	sqlTask, err := app.CreateTask(CreateTaskRequest{
		Name:        "GUI E2E SQL 执行",
		Description: "从新 GUI 任务 API 执行 SQL",
		Kind:        "sql",
		Request:     sqlReq,
	})
	if err != nil {
		t.Fatalf("CreateTask(sql) failed: %v", err)
	}
	if _, err := app.StartTask(sqlTask.ID); err != nil {
		t.Fatalf("StartTask(sql) failed: %v", err)
	}
	sqlTask = waitForTask(t, app, sqlTask.ID, 30*time.Second)
	assertCompletedTask(t, sqlTask)
	if sqlTask.State.SQLResult == nil || sqlTask.State.SQLResult.Total != 1 {
		t.Fatalf("unexpected SQL result: %#v", sqlTask.State.SQLResult)
	}

	fscanText := "mysql " + host + ":" + itoa(mysqlPort) + " root:scanpass\n" +
		"postgres " + host + ":" + itoa(postgresPort) + " audit:scanpass"
	preview, err := app.ParseFscanText(fscanText)
	if err != nil {
		t.Fatalf("ParseFscanText failed: %v", err)
	}
	if preview.Total != 2 {
		t.Fatalf("expected 2 fscan targets, got %#v", preview)
	}
	fscanOutput := filepath.Join(t.TempDir(), "gui-fscan.xlsx")
	fscanTask, err := app.CreateTask(CreateTaskRequest{
		Name:        "GUI E2E fscan 批量扫描",
		Description: "从新 GUI 任务 API 批量扫描虚拟机 MySQL/Postgres",
		Kind:        "fscan",
		Request: coreapp.ScanRequest{
			FscanText:   fscanText,
			Mode:        "field-content",
			Level:       "all",
			Limit:       3,
			Workers:     2,
			Timeout:     "10s",
			Output:      fscanOutput,
			SplitOutput: true,
		},
	})
	if err != nil {
		t.Fatalf("CreateTask(fscan) failed: %v", err)
	}
	if _, err := app.StartTask(fscanTask.ID); err != nil {
		t.Fatalf("StartTask(fscan) failed: %v", err)
	}
	fscanTask = waitForTask(t, app, fscanTask.ID, 90*time.Second)
	assertCompletedTask(t, fscanTask)
	if len(fscanTask.State.Outputs) < 3 {
		t.Fatalf("expected summary and split outputs, got %#v", fscanTask.State.Outputs)
	}
	for _, output := range fscanTask.State.Outputs {
		assertFileExists(t, output)
	}

	if err := app.DeleteTask(singleTask.ID); err != nil {
		t.Fatalf("DeleteTask(single) failed: %v", err)
	}
	tasks, err := app.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}
	for _, task := range tasks {
		if task.ID == singleTask.ID {
			t.Fatalf("deleted task %s still present", singleTask.ID)
		}
	}
}

func waitForTask(t *testing.T, app *App, id string, timeout time.Duration) GUITask {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		task, err := app.GetTask(id)
		if err != nil {
			t.Fatalf("GetTask(%s) failed: %v", id, err)
		}
		if task.Status != "running" {
			return task
		}
		time.Sleep(250 * time.Millisecond)
	}
	task, _ := app.GetTask(id)
	t.Fatalf("task %s did not finish before timeout; last status=%s message=%s", id, task.Status, task.Message)
	return GUITask{}
}

func assertCompletedTask(t *testing.T, task GUITask) {
	t.Helper()
	if task.Status != "completed" || task.Progress != 100 {
		t.Fatalf("task did not complete: status=%s progress=%d message=%s errors=%v", task.Status, task.Progress, task.Message, task.State.Errors)
	}
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected output file %s: %v", path, err)
	}
	if info.Size() == 0 {
		t.Fatalf("output file %s is empty", path)
	}
}

func envOr(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envOrInt(key string, fallback int) int {
	if value := os.Getenv(key); value != "" {
		var out int
		if _, err := fmt.Sscanf(value, "%d", &out); err == nil && out > 0 {
			return out
		}
	}
	return fallback
}

func itoa(value int) string {
	return strconv.Itoa(value)
}
