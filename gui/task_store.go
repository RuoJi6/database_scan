package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	coreapp "database_scan/internal/app"

	"github.com/google/uuid"
	_ "github.com/mutecomm/go-sqlcipher/v4"
	"golang.org/x/crypto/argon2"
)

const (
	vaultVersion       = 2
	backupVersion      = 1
	currentSchema      = 1
	kdfTime            = uint32(3)
	kdfMemory          = uint32(64 * 1024)
	kdfThreads         = uint8(4)
	kdfKeyLen          = uint32(32)
	sqlCipherPageSize  = "4096"
	backupNameTimeFmt  = "20060102-1504"
	backupMinPassBytes = 6
)

type VaultStatus struct {
	Initialized bool
	Unlocked    bool
	Path        string
}

type CreateTaskRequest struct {
	Name        string
	Description string
	Kind        string
	Request     coreapp.ScanRequest
}

type UpdateTaskRequest struct {
	ID          string
	Name        string
	Description string
	Kind        string
	Request     coreapp.ScanRequest
}

type BackupExportRequest struct {
	Path     string
	Encrypt  bool
	Password string
}

type BackupImportRequest struct {
	Path     string
	Password string
}

type BackupResult struct {
	Path          string
	Encrypted     bool
	ExportedTasks int
	ImportedTasks int
	RenamedTasks  int
	Message       string
}

type GUITask struct {
	ID          string
	Name        string
	Description string
	Kind        string
	Status      string
	Progress    int
	Message     string
	TargetLabel string
	Request     coreapp.ScanRequest
	State       coreapp.ScanJobState
	CreatedAt   string
	UpdatedAt   string
	StartedAt   string
	FinishedAt  string
}

type kdfParams struct {
	Name    string `json:"name"`
	Time    uint32 `json:"time"`
	Memory  uint32 `json:"memory"`
	Threads uint8  `json:"threads"`
	KeyLen  uint32 `json:"keyLen"`
}

type backupEnvelope struct {
	Version   int            `json:"version"`
	Format    string         `json:"format"`
	Encrypted bool           `json:"encrypted"`
	KDF       kdfParams      `json:"kdf,omitempty"`
	Salt      string         `json:"salt,omitempty"`
	Nonce     string         `json:"nonce,omitempty"`
	Data      string         `json:"data,omitempty"`
	Payload   *backupPayload `json:"payload,omitempty"`
}

type backupPayload struct {
	Version    int       `json:"version"`
	ExportedAt string    `json:"exportedAt"`
	Tasks      []GUITask `json:"tasks"`
}

type taskVault struct {
	mu       sync.Mutex
	path     string
	db       *sql.DB
	password string
	unlocked bool
}

func newTaskVault() (*taskVault, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	return &taskVault{path: filepath.Join(configDir, "database_scan", "database_scan.db")}, nil
}

func (v *taskVault) status() VaultStatus {
	v.mu.Lock()
	defer v.mu.Unlock()
	_, err := os.Stat(v.path)
	return VaultStatus{Initialized: err == nil, Unlocked: v.unlocked, Path: v.path}
}

func (v *taskVault) setup(password string) (VaultStatus, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if strings.TrimSpace(password) == "" {
		return VaultStatus{}, errors.New("password is required")
	}
	if err := os.MkdirAll(filepath.Dir(v.path), 0o700); err != nil {
		return VaultStatus{}, err
	}
	if v.db != nil {
		_ = v.db.Close()
	}
	if err := removeVaultFiles(v.path); err != nil {
		return VaultStatus{}, err
	}
	db, err := openEncryptedDB(v.path, password)
	if err != nil {
		return VaultStatus{}, err
	}
	if err := migrateDB(db); err != nil {
		_ = db.Close()
		return VaultStatus{}, err
	}
	v.db = db
	v.password = password
	v.unlocked = true
	return VaultStatus{Initialized: true, Unlocked: true, Path: v.path}, nil
}

func (v *taskVault) unlock(password string) (VaultStatus, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if strings.TrimSpace(password) == "" {
		return VaultStatus{}, errors.New("password is required")
	}
	if _, err := os.Stat(v.path); err != nil {
		return VaultStatus{}, errors.New("vault is not initialized")
	}
	db, err := openEncryptedDB(v.path, password)
	if err != nil {
		return VaultStatus{}, errors.New("password is incorrect or vault is corrupted")
	}
	if err := migrateDB(db); err != nil {
		_ = db.Close()
		return VaultStatus{}, errors.New("password is incorrect or vault is corrupted")
	}
	if v.db != nil {
		_ = v.db.Close()
	}
	v.db = db
	v.password = password
	v.unlocked = true
	return VaultStatus{Initialized: true, Unlocked: true, Path: v.path}, nil
}

func (v *taskVault) reset() (VaultStatus, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.db != nil {
		_ = v.db.Close()
	}
	v.db = nil
	v.password = ""
	v.unlocked = false
	if err := removeVaultFiles(v.path); err != nil {
		return VaultStatus{}, err
	}
	return VaultStatus{Initialized: false, Unlocked: false, Path: v.path}, nil
}

func (v *taskVault) listTasks() ([]GUITask, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.requireUnlocked(); err != nil {
		return nil, err
	}
	tasks, err := v.listTasksLocked()
	if err != nil {
		return nil, err
	}
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].UpdatedAt > tasks[j].UpdatedAt
	})
	return tasks, nil
}

func (v *taskVault) getTask(id string) (GUITask, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.requireUnlocked(); err != nil {
		return GUITask{}, err
	}
	return v.getTaskLocked(id)
}

func (v *taskVault) createTask(req CreateTaskRequest) (GUITask, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.requireUnlocked(); err != nil {
		return GUITask{}, err
	}
	now := time.Now().Format(time.RFC3339)
	task := GUITask{
		ID:          uuid.NewString(),
		Name:        strings.TrimSpace(req.Name),
		Description: strings.TrimSpace(req.Description),
		Kind:        normalizeTaskKind(req.Kind),
		Status:      "draft",
		Progress:    0,
		Message:     "任务已创建，等待启动",
		Request:     normalizeTaskRequest(req.Request, req.Kind),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if task.Name == "" {
		return GUITask{}, errors.New("task name is required")
	}
	task.State = coreapp.ScanJobState{Status: task.Status, Progress: task.Progress, Message: task.Message, Request: task.Request}
	if err := v.upsertTaskLocked(task); err != nil {
		return GUITask{}, err
	}
	return cloneTask(task), nil
}

func (v *taskVault) updateTask(req UpdateTaskRequest) (GUITask, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.requireUnlocked(); err != nil {
		return GUITask{}, err
	}
	task, err := v.getTaskLocked(req.ID)
	if err != nil {
		return GUITask{}, err
	}
	if strings.TrimSpace(req.Name) == "" {
		return GUITask{}, errors.New("task name is required")
	}
	task.Name = strings.TrimSpace(req.Name)
	task.Description = strings.TrimSpace(req.Description)
	task.Kind = normalizeTaskKind(req.Kind)
	task.Request = normalizeTaskRequest(req.Request, req.Kind)
	task.State.Request = task.Request
	task.UpdatedAt = time.Now().Format(time.RFC3339)
	if err := v.upsertTaskLocked(task); err != nil {
		return GUITask{}, err
	}
	return cloneTask(task), nil
}

func (v *taskVault) deleteTask(id string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.requireUnlocked(); err != nil {
		return err
	}
	result, err := v.db.Exec(`DELETE FROM tasks WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return fmt.Errorf("task %s not found", id)
	}
	return nil
}

func (v *taskVault) replaceTask(task GUITask) (GUITask, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.requireUnlocked(); err != nil {
		return GUITask{}, err
	}
	if _, err := v.getTaskLocked(task.ID); err != nil {
		return GUITask{}, err
	}
	task.UpdatedAt = time.Now().Format(time.RFC3339)
	if err := v.upsertTaskLocked(task); err != nil {
		return GUITask{}, err
	}
	return cloneTask(task), nil
}

func (v *taskVault) exportBackup(req BackupExportRequest) (BackupResult, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.requireUnlocked(); err != nil {
		return BackupResult{}, err
	}
	if strings.TrimSpace(req.Path) == "" {
		return BackupResult{}, errors.New("backup path is required")
	}
	tasks, err := v.listTasksLocked()
	if err != nil {
		return BackupResult{}, err
	}
	payload := backupPayload{Version: backupVersion, ExportedAt: time.Now().Format(time.RFC3339), Tasks: tasks}
	envelope := backupEnvelope{Version: backupVersion, Format: "database_scan.backup", Encrypted: req.Encrypt}
	if req.Encrypt {
		if len(req.Password) < backupMinPassBytes {
			return BackupResult{}, errors.New("backup password must be at least 6 characters")
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			return BackupResult{}, err
		}
		salt, err := randomBytes(16)
		if err != nil {
			return BackupResult{}, err
		}
		key := deriveBackupKey(req.Password, salt)
		block, err := aes.NewCipher(key)
		if err != nil {
			return BackupResult{}, err
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return BackupResult{}, err
		}
		nonce, err := randomBytes(aead.NonceSize())
		if err != nil {
			return BackupResult{}, err
		}
		envelope.KDF = kdfParams{Name: "argon2id", Time: kdfTime, Memory: kdfMemory, Threads: kdfThreads, KeyLen: kdfKeyLen}
		envelope.Salt = base64.StdEncoding.EncodeToString(salt)
		envelope.Nonce = base64.StdEncoding.EncodeToString(nonce)
		envelope.Data = base64.StdEncoding.EncodeToString(aead.Seal(nil, nonce, raw, nil))
	} else {
		envelope.Payload = &payload
	}
	out, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return BackupResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(req.Path), 0o700); err != nil {
		return BackupResult{}, err
	}
	if err := os.WriteFile(req.Path, out, 0o600); err != nil {
		return BackupResult{}, err
	}
	return BackupResult{Path: req.Path, Encrypted: req.Encrypt, ExportedTasks: len(tasks), Message: "备份导出完成"}, nil
}

func (v *taskVault) importBackup(req BackupImportRequest) (BackupResult, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.requireUnlocked(); err != nil {
		return BackupResult{}, err
	}
	if strings.TrimSpace(req.Path) == "" {
		return BackupResult{}, errors.New("backup path is required")
	}
	payload, encrypted, err := readBackupPayload(req.Path, req.Password)
	if err != nil {
		return BackupResult{}, err
	}
	existing, err := v.listTasksLocked()
	if err != nil {
		return BackupResult{}, err
	}
	tx, err := v.db.Begin()
	if err != nil {
		return BackupResult{}, err
	}
	defer tx.Rollback()

	ids := map[string]bool{}
	names := map[string]bool{}
	for _, task := range existing {
		ids[task.ID] = true
		names[task.Name] = true
	}
	renamed := 0
	importedAt := time.Now().Format(backupNameTimeFmt)
	for _, task := range payload.Tasks {
		if ids[task.ID] || task.ID == "" {
			task.ID = uuid.NewString()
			task.State.JobID = task.ID
		}
		originalName := strings.TrimSpace(task.Name)
		if originalName == "" {
			originalName = "导入任务"
		}
		task.Name = uniqueImportedName(originalName, names, importedAt)
		if task.Name != originalName {
			renamed++
		}
		names[task.Name] = true
		ids[task.ID] = true
		if task.CreatedAt == "" {
			task.CreatedAt = time.Now().Format(time.RFC3339)
		}
		task.UpdatedAt = time.Now().Format(time.RFC3339)
		if err := upsertTaskTx(tx, task); err != nil {
			return BackupResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return BackupResult{}, err
	}
	return BackupResult{Path: req.Path, Encrypted: encrypted, ImportedTasks: len(payload.Tasks), RenamedTasks: renamed, Message: "备份导入完成"}, nil
}

func (v *taskVault) requireUnlocked() error {
	if !v.unlocked || v.db == nil {
		return errors.New("vault is locked")
	}
	return nil
}

func (v *taskVault) listTasksLocked() ([]GUITask, error) {
	rows, err := v.db.Query(`SELECT task_json FROM tasks ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []GUITask
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var task GUITask
		if err := json.Unmarshal([]byte(raw), &task); err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	if tasks == nil {
		return []GUITask{}, nil
	}
	return tasks, rows.Err()
}

func (v *taskVault) getTaskLocked(id string) (GUITask, error) {
	var raw string
	err := v.db.QueryRow(`SELECT task_json FROM tasks WHERE id = ?`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return GUITask{}, fmt.Errorf("task %s not found", id)
	}
	if err != nil {
		return GUITask{}, err
	}
	var task GUITask
	if err := json.Unmarshal([]byte(raw), &task); err != nil {
		return GUITask{}, err
	}
	return task, nil
}

func (v *taskVault) upsertTaskLocked(task GUITask) error {
	tx, err := v.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := upsertTaskTx(tx, task); err != nil {
		return err
	}
	return tx.Commit()
}

func openEncryptedDB(path, password string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	dsn := path + "?_pragma_key=" + url.QueryEscape(password) + "&_pragma_cipher_page_size=" + sqlCipherPageSize + "&_busy_timeout=5000"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(`SELECT count(*) FROM sqlite_master`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func migrateDB(db *sql.DB) error {
	statements := []struct {
		sql  string
		args []any
	}{
		{sql: `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`},
		{sql: `CREATE TABLE IF NOT EXISTS tasks (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT NOT NULL,
			kind TEXT NOT NULL,
			status TEXT NOT NULL,
			progress INTEGER NOT NULL,
			message TEXT NOT NULL,
			target_label TEXT NOT NULL,
			request_json TEXT NOT NULL,
			state_json TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			started_at TEXT NOT NULL,
			finished_at TEXT NOT NULL,
			task_json TEXT NOT NULL
		)`},
		{sql: `CREATE TABLE IF NOT EXISTS task_configs (task_id TEXT PRIMARY KEY, request_json TEXT NOT NULL, FOREIGN KEY(task_id) REFERENCES tasks(id) ON DELETE CASCADE)`},
		{sql: `CREATE TABLE IF NOT EXISTS task_runs (task_id TEXT PRIMARY KEY, status TEXT NOT NULL, progress INTEGER NOT NULL, message TEXT NOT NULL, started_at TEXT NOT NULL, finished_at TEXT NOT NULL, FOREIGN KEY(task_id) REFERENCES tasks(id) ON DELETE CASCADE)`},
		{sql: `CREATE TABLE IF NOT EXISTS scan_tables (id INTEGER PRIMARY KEY AUTOINCREMENT, task_id TEXT NOT NULL, table_order INTEGER NOT NULL, database_name TEXT NOT NULL, schema_name TEXT NOT NULL, table_name TEXT NOT NULL, total INTEGER NOT NULL, target_label TEXT NOT NULL, FOREIGN KEY(task_id) REFERENCES tasks(id) ON DELETE CASCADE)`},
		{sql: `CREATE TABLE IF NOT EXISTS scan_fields (table_id INTEGER NOT NULL, field_order INTEGER NOT NULL, name TEXT NOT NULL, kinds_json TEXT NOT NULL, level TEXT NOT NULL, mode TEXT NOT NULL, total INTEGER NOT NULL, FOREIGN KEY(table_id) REFERENCES scan_tables(id) ON DELETE CASCADE)`},
		{sql: `CREATE TABLE IF NOT EXISTS scan_samples (table_id INTEGER NOT NULL, row_order INTEGER NOT NULL, values_json TEXT NOT NULL, FOREIGN KEY(table_id) REFERENCES scan_tables(id) ON DELETE CASCADE)`},
		{sql: `CREATE TABLE IF NOT EXISTS task_logs (task_id TEXT NOT NULL, log_order INTEGER NOT NULL, time TEXT NOT NULL, level TEXT NOT NULL, message TEXT NOT NULL, FOREIGN KEY(task_id) REFERENCES tasks(id) ON DELETE CASCADE)`},
		{sql: `CREATE TABLE IF NOT EXISTS outputs (task_id TEXT NOT NULL, path TEXT NOT NULL, created_at TEXT NOT NULL, FOREIGN KEY(task_id) REFERENCES tasks(id) ON DELETE CASCADE)`},
		{sql: `CREATE INDEX IF NOT EXISTS idx_scan_tables_task ON scan_tables(task_id)`},
		{sql: `CREATE INDEX IF NOT EXISTS idx_task_logs_task ON task_logs(task_id)`},
		{sql: `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(1, ?)`, args: []any{time.Now().Format(time.RFC3339)}},
	}
	for _, stmt := range statements {
		if _, err := db.Exec(stmt.sql, stmt.args...); err != nil {
			return err
		}
	}
	return nil
}

func upsertTaskTx(tx *sql.Tx, task GUITask) error {
	requestJSON, err := json.Marshal(task.Request)
	if err != nil {
		return err
	}
	stateJSON, err := json.Marshal(task.State)
	if err != nil {
		return err
	}
	taskJSON, err := json.Marshal(task)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO tasks(id, name, description, kind, status, progress, message, target_label, request_json, state_json, created_at, updated_at, started_at, finished_at, task_json)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name,
			description=excluded.description,
			kind=excluded.kind,
			status=excluded.status,
			progress=excluded.progress,
			message=excluded.message,
			target_label=excluded.target_label,
			request_json=excluded.request_json,
			state_json=excluded.state_json,
			created_at=excluded.created_at,
			updated_at=excluded.updated_at,
			started_at=excluded.started_at,
			finished_at=excluded.finished_at,
			task_json=excluded.task_json`,
		task.ID, task.Name, task.Description, task.Kind, task.Status, task.Progress, task.Message, task.TargetLabel,
		string(requestJSON), string(stateJSON), task.CreatedAt, task.UpdatedAt, task.StartedAt, task.FinishedAt, string(taskJSON))
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM task_configs WHERE task_id = ?`, task.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM task_runs WHERE task_id = ?`, task.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM scan_tables WHERE task_id = ?`, task.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM task_logs WHERE task_id = ?`, task.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM outputs WHERE task_id = ?`, task.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO task_configs(task_id, request_json) VALUES(?, ?)`, task.ID, string(requestJSON)); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO task_runs(task_id, status, progress, message, started_at, finished_at) VALUES(?, ?, ?, ?, ?, ?)`, task.ID, task.Status, task.Progress, task.Message, task.StartedAt, task.FinishedAt); err != nil {
		return err
	}
	for index, entry := range task.State.Logs {
		if _, err := tx.Exec(`INSERT INTO task_logs(task_id, log_order, time, level, message) VALUES(?, ?, ?, ?, ?)`, task.ID, index, entry.Time, entry.Level, entry.Message); err != nil {
			return err
		}
	}
	for _, output := range task.State.Outputs {
		if _, err := tx.Exec(`INSERT INTO outputs(task_id, path, created_at) VALUES(?, ?, ?)`, task.ID, output, time.Now().Format(time.RFC3339)); err != nil {
			return err
		}
	}
	for tableIndex, table := range task.State.Result.Tables {
		result, err := tx.Exec(`INSERT INTO scan_tables(task_id, table_order, database_name, schema_name, table_name, total, target_label) VALUES(?, ?, ?, ?, ?, ?, ?)`, task.ID, tableIndex, table.Database, table.Schema, table.Name, table.Total, task.TargetLabel)
		if err != nil {
			return err
		}
		tableID, err := result.LastInsertId()
		if err != nil {
			return err
		}
		for fieldIndex, field := range table.Fields {
			kindsJSON, err := json.Marshal(field.Kinds)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO scan_fields(table_id, field_order, name, kinds_json, level, mode, total) VALUES(?, ?, ?, ?, ?, ?, ?)`, tableID, fieldIndex, field.Name, string(kindsJSON), field.Level, field.Mode, field.Total); err != nil {
				return err
			}
		}
		for rowIndex, row := range table.Rows {
			valuesJSON, err := json.Marshal(row.Values)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO scan_samples(table_id, row_order, values_json) VALUES(?, ?, ?)`, tableID, rowIndex, string(valuesJSON)); err != nil {
				return err
			}
		}
	}
	return nil
}

func readBackupPayload(path, password string) (backupPayload, bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return backupPayload{}, false, err
	}
	var envelope backupEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return backupPayload{}, false, err
	}
	if envelope.Format != "database_scan.backup" {
		return backupPayload{}, false, errors.New("unsupported backup file")
	}
	if !envelope.Encrypted {
		if envelope.Payload == nil {
			return backupPayload{}, false, errors.New("backup payload is missing")
		}
		return *envelope.Payload, false, nil
	}
	if password == "" {
		return backupPayload{}, true, errors.New("backup password is required")
	}
	salt, err := base64.StdEncoding.DecodeString(envelope.Salt)
	if err != nil {
		return backupPayload{}, true, err
	}
	nonce, err := base64.StdEncoding.DecodeString(envelope.Nonce)
	if err != nil {
		return backupPayload{}, true, err
	}
	ciphertext, err := base64.StdEncoding.DecodeString(envelope.Data)
	if err != nil {
		return backupPayload{}, true, err
	}
	block, err := aes.NewCipher(deriveBackupKey(password, salt))
	if err != nil {
		return backupPayload{}, true, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return backupPayload{}, true, err
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return backupPayload{}, true, errors.New("backup password is incorrect or file is corrupted")
	}
	var payload backupPayload
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return backupPayload{}, true, err
	}
	return payload, true, nil
}

func uniqueImportedName(name string, existing map[string]bool, importedAt string) string {
	if !existing[name] {
		return name
	}
	base := fmt.Sprintf("%s (导入 %s)", name, importedAt)
	if !existing[base] {
		return base
	}
	for i := 2; ; i++ {
		next := fmt.Sprintf("%s (%d)", base, i)
		if !existing[next] {
			return next
		}
	}
}

func deriveBackupKey(password string, salt []byte) []byte {
	return argon2.IDKey([]byte(password), salt, kdfTime, kdfMemory, kdfThreads, kdfKeyLen)
}

func randomBytes(size int) ([]byte, error) {
	out := make([]byte, size)
	_, err := rand.Read(out)
	return out, err
}

func removeVaultFiles(path string) error {
	for _, candidate := range []string{path, path + "-wal", path + "-shm", path + "-journal"} {
		if err := os.Remove(candidate); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func normalizeTaskKind(kind string) string {
	switch strings.TrimSpace(kind) {
	case "fscan", "sql":
		return strings.TrimSpace(kind)
	default:
		return "single"
	}
}

func normalizeTaskRequest(req coreapp.ScanRequest, kind string) coreapp.ScanRequest {
	defaults := coreapp.DefaultScanRequest()
	if req.Type == "" {
		req.Type = defaults.Type
	}
	if req.Mode == "" {
		req.Mode = defaults.Mode
	}
	if req.Level == "" {
		req.Level = defaults.Level
	}
	if req.Limit == 0 {
		req.Limit = defaults.Limit
	}
	if req.Workers == 0 {
		req.Workers = defaults.Workers
	}
	if req.Timeout == "" {
		req.Timeout = defaults.Timeout
	}
	switch normalizeTaskKind(kind) {
	case "fscan":
		req.Type = ""
		req.Host = ""
		req.User = ""
		req.Password = ""
		req.SQL = ""
	case "sql":
		req.Fscan = ""
		req.FscanText = ""
	default:
		req.Fscan = ""
		req.FscanText = ""
		req.SQL = ""
	}
	return req
}

func cloneTask(task GUITask) GUITask {
	raw, _ := json.Marshal(task)
	var out GUITask
	_ = json.Unmarshal(raw, &out)
	return out
}

func cloneTasks(tasks []GUITask) []GUITask {
	raw, _ := json.Marshal(tasks)
	var out []GUITask
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		return []GUITask{}
	}
	return out
}
