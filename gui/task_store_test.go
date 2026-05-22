package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	coreapp "database_scan/internal/app"
)

func TestTaskVaultRoundTripAndEncryption(t *testing.T) {
	vault := testVault(t)
	if _, err := vault.setup("correct horse battery staple"); err != nil {
		t.Fatalf("setup vault: %v", err)
	}
	task, err := vault.createTask(CreateTaskRequest{
		Name:        "Secret customer audit",
		Description: "contains sensitive connection data",
		Kind:        "single",
		Request: coreapp.ScanRequest{
			Type:     "mysql",
			Host:     "127.0.0.1",
			Port:     3306,
			User:     "root",
			Password: "super-secret-password",
			Mode:     "field-content",
			Level:    "all",
			Limit:    15,
			Workers:  1,
			Timeout:  "15s",
		},
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	raw, err := os.ReadFile(vault.path)
	if err != nil {
		t.Fatalf("read vault file: %v", err)
	}
	for _, forbidden := range []string{"Secret customer audit", "127.0.0.1", "super-secret-password"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("vault file contains plaintext %q", forbidden)
		}
	}

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
	if tasks[0].ID != task.ID || tasks[0].Request.Password != "super-secret-password" {
		t.Fatalf("round trip task mismatch: %#v", tasks[0])
	}
}

func TestTaskVaultCRUDAndReset(t *testing.T) {
	vault := testVault(t)
	if _, err := vault.setup("local password"); err != nil {
		t.Fatalf("setup vault: %v", err)
	}
	task, err := vault.createTask(CreateTaskRequest{
		Name: "Draft audit",
		Kind: "sql",
		Request: coreapp.ScanRequest{
			Type:    "postgres",
			Host:    "db.internal",
			Port:    5432,
			User:    "audit",
			SQL:     "select 1",
			Mode:    "field-content",
			Level:   "all",
			Limit:   15,
			Workers: 1,
			Timeout: "15s",
		},
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

func testVault(t *testing.T) *taskVault {
	t.Helper()
	return &taskVault{path: filepath.Join(t.TempDir(), "tasks.enc")}
}
