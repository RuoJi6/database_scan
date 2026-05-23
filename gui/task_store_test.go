package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	coreapp "database_scan/internal/app"
	"database_scan/internal/detector"
	"database_scan/internal/scanner"
)

func TestTaskVaultSQLCipherRoundTripAndEncryption(t *testing.T) {
	vault := testVault(t)
	if _, err := vault.setup("correct horse battery staple"); err != nil {
		t.Fatalf("setup vault: %v", err)
	}
	task, err := vault.createTask(CreateTaskRequest{
		Name:        "Secret customer audit",
		Description: "contains sensitive connection data",
		Kind:        "single",
		Request:     secretRequest(),
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	task.State.Result.Tables = []scanner.TableResult{sampleTable()}
	if _, err := vault.replaceTask(task); err != nil {
		t.Fatalf("replace task with results: %v", err)
	}
	assertFileDoesNotContain(t, vault.path, "Secret customer audit", "127.0.0.1", "super-secret-password", "sk_live_demo")

	reopened := &taskVault{path: vault.path}
	if _, err := reopened.unlock("wrong password"); err == nil {
		t.Fatalf("unlock with wrong password succeeded")
	}
	if _, err := reopened.unlock("correct horse battery staple"); err != nil {
		t.Fatalf("unlock with correct password: %v", err)
	}
	tasks, err := reopened.listTasks()
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("got %d tasks, want 1", len(tasks))
	}
	got := tasks[0]
	if got.ID != task.ID || got.Request.Password != "super-secret-password" {
		t.Fatalf("round trip task mismatch: %#v", got)
	}
	if len(got.State.Result.Tables) != 1 || got.State.Result.Tables[0].Rows[0].Values["secret_key"] != "sk_live_demo" {
		t.Fatalf("round trip scan result mismatch: %#v", got.State.Result.Tables)
	}
}

func TestTaskVaultCRUDAndReset(t *testing.T) {
	vault := testVault(t)
	if _, err := vault.setup("local password"); err != nil {
		t.Fatalf("setup vault: %v", err)
	}
	task, err := vault.createTask(CreateTaskRequest{
		Name:    "Draft audit",
		Kind:    "sql",
		Request: sqlRequest(),
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	updated, err := vault.updateTask(UpdateTaskRequest{
		ID:          task.ID,
		Name:        "Updated audit",
		Description: "run sql safely",
		Kind:        "sql",
		Request:     task.Request,
	})
	if err != nil {
		t.Fatalf("update task: %v", err)
	}
	if updated.Name != "Updated audit" || updated.Description != "run sql safely" {
		t.Fatalf("task not updated: %#v", updated)
	}
	if err := vault.deleteTask(task.ID); err != nil {
		t.Fatalf("delete task: %v", err)
	}
	tasks, err := vault.listTasks()
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("got %d tasks after delete, want 0", len(tasks))
	}
	if _, err := vault.reset(); err != nil {
		t.Fatalf("reset vault: %v", err)
	}
	if _, err := os.Stat(vault.path); !os.IsNotExist(err) {
		t.Fatalf("vault file still exists after reset")
	}
}

func TestTaskVaultPlainBackupMergeRenamesConflicts(t *testing.T) {
	vault := seededVault(t, "backup-pass")
	backupPath := filepath.Join(t.TempDir(), "plain.dbsbak")
	exported, err := vault.exportBackup(BackupExportRequest{Path: backupPath})
	if err != nil {
		t.Fatalf("export plain backup: %v", err)
	}
	if exported.ExportedTasks != 1 || exported.Encrypted {
		t.Fatalf("unexpected export result: %#v", exported)
	}
	assertFileContains(t, backupPath, "Secret customer audit", "super-secret-password")

	imported, err := vault.importBackup(BackupImportRequest{Path: backupPath})
	if err != nil {
		t.Fatalf("import plain backup: %v", err)
	}
	if imported.ImportedTasks != 1 || imported.RenamedTasks != 1 {
		t.Fatalf("unexpected import result: %#v", imported)
	}
	tasks, err := vault.listTasks()
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("got %d tasks after merge import, want 2", len(tasks))
	}
	ids := map[string]bool{}
	renamed := false
	for _, task := range tasks {
		if ids[task.ID] {
			t.Fatalf("duplicate task id after import: %s", task.ID)
		}
		ids[task.ID] = true
		if strings.Contains(task.Name, "导入 ") {
			renamed = true
		}
	}
	if !renamed {
		t.Fatalf("expected one imported task to be renamed: %#v", tasks)
	}
}

func TestTaskVaultEncryptedBackupAndWrongPasswordRollback(t *testing.T) {
	vault := seededVault(t, "backup-pass")
	backupPath := filepath.Join(t.TempDir(), "encrypted.dbsbak")
	exported, err := vault.exportBackup(BackupExportRequest{Path: backupPath, Encrypt: true, Password: "backup-secret"})
	if err != nil {
		t.Fatalf("export encrypted backup: %v", err)
	}
	if !exported.Encrypted || exported.ExportedTasks != 1 {
		t.Fatalf("unexpected encrypted export result: %#v", exported)
	}
	assertFileDoesNotContain(t, backupPath, "Secret customer audit", "127.0.0.1", "super-secret-password", "sk_live_demo")

	if _, err := vault.importBackup(BackupImportRequest{Path: backupPath, Password: "wrong-secret"}); err == nil {
		t.Fatalf("import encrypted backup with wrong password succeeded")
	}
	tasks, err := vault.listTasks()
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("wrong password import changed local DB, got %d tasks", len(tasks))
	}

	imported, err := vault.importBackup(BackupImportRequest{Path: backupPath, Password: "backup-secret"})
	if err != nil {
		t.Fatalf("import encrypted backup: %v", err)
	}
	if imported.ImportedTasks != 1 || imported.RenamedTasks != 1 || !imported.Encrypted {
		t.Fatalf("unexpected encrypted import result: %#v", imported)
	}
}

func seededVault(t *testing.T, password string) *taskVault {
	t.Helper()
	vault := testVault(t)
	if _, err := vault.setup(password); err != nil {
		t.Fatalf("setup vault: %v", err)
	}
	task, err := vault.createTask(CreateTaskRequest{
		Name:        "Secret customer audit",
		Description: "contains sensitive connection data",
		Kind:        "single",
		Request:     secretRequest(),
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	task.Status = "completed"
	task.Progress = 100
	task.State = coreapp.ScanJobState{
		JobID:    task.ID,
		Status:   "completed",
		Progress: 100,
		Message:  "done",
		Request:  task.Request,
		Result:   scanner.Result{Tables: []scanner.TableResult{sampleTable()}},
		Logs:     []coreapp.LogEntry{{Time: "10:01:03", Level: "info", Message: "scan complete"}},
		Outputs:  []string{"/tmp/database_scan_report.xlsx"},
	}
	if _, err := vault.replaceTask(task); err != nil {
		t.Fatalf("replace task: %v", err)
	}
	return vault
}

func secretRequest() coreapp.ScanRequest {
	return coreapp.ScanRequest{
		Type:     "mysql",
		Host:     "127.0.0.1",
		Port:     3306,
		User:     "root",
		Password: "super-secret-password",
		Database: "audit_lab",
		Mode:     "field-content",
		Level:    "all",
		Limit:    15,
		Workers:  1,
		Timeout:  "15s",
	}
}

func sqlRequest() coreapp.ScanRequest {
	req := secretRequest()
	req.Type = "postgres"
	req.Host = "db.internal"
	req.Port = 5432
	req.User = "audit"
	req.SQL = "select 1"
	return req
}

func sampleTable() scanner.TableResult {
	return scanner.TableResult{
		Database: "audit_lab",
		Name:     "access_tokens",
		Total:    1,
		Columns:  []string{"id", "secret_key"},
		Fields: []scanner.FieldResult{
			{Name: "secret_key", Kinds: []detector.Kind{detector.Password}, Level: detector.LevelHigh, Mode: scanner.FieldContent, Total: 1},
		},
		Rows: []scanner.RowSample{{Values: map[string]string{"id": "1", "secret_key": "sk_live_demo"}}},
	}
}

func assertFileDoesNotContain(t *testing.T, path string, forbidden ...string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	text := string(raw)
	for _, value := range forbidden {
		if strings.Contains(text, value) {
			t.Fatalf("%s contains plaintext %q", path, value)
		}
	}
}

func assertFileContains(t *testing.T, path string, expected ...string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	text := string(raw)
	for _, value := range expected {
		if !strings.Contains(text, value) {
			t.Fatalf("%s does not contain expected plaintext %q", path, value)
		}
	}
}

func testVault(t *testing.T) *taskVault {
	t.Helper()
	return &taskVault{path: filepath.Join(t.TempDir(), "database_scan.db")}
}
