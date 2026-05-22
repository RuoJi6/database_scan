package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	coreapp "database_scan/internal/app"

	"github.com/google/uuid"
	"golang.org/x/crypto/argon2"
)

const (
	vaultVersion = 1
	kdfTime      = uint32(3)
	kdfMemory    = uint32(64 * 1024)
	kdfThreads   = uint8(4)
	kdfKeyLen    = uint32(32)
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

type taskPayload struct {
	Tasks []GUITask
}

type vaultEnvelope struct {
	Version int       `json:"version"`
	KDF     kdfParams `json:"kdf"`
	Salt    string    `json:"salt"`
	Nonce   string    `json:"nonce"`
	Data    string    `json:"data"`
}

type kdfParams struct {
	Name    string `json:"name"`
	Time    uint32 `json:"time"`
	Memory  uint32 `json:"memory"`
	Threads uint8  `json:"threads"`
	KeyLen  uint32 `json:"keyLen"`
}

type taskVault struct {
	mu       sync.Mutex
	path     string
	key      []byte
	payload  taskPayload
	unlocked bool
}

func newTaskVault() (*taskVault, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	return &taskVault{path: filepath.Join(configDir, "database_scan", "tasks.enc")}, nil
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
	salt, err := randomBytes(16)
	if err != nil {
		return VaultStatus{}, err
	}
	v.key = deriveVaultKey(password, salt)
	v.payload = taskPayload{Tasks: []GUITask{}}
	v.unlocked = true
	if err := v.saveLocked(salt); err != nil {
		v.key = nil
		v.unlocked = false
		return VaultStatus{}, err
	}
	return VaultStatus{Initialized: true, Unlocked: true, Path: v.path}, nil
}

func (v *taskVault) unlock(password string) (VaultStatus, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	raw, err := os.ReadFile(v.path)
	if err != nil {
		return VaultStatus{}, err
	}
	var envelope vaultEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return VaultStatus{}, err
	}
	salt, err := base64.StdEncoding.DecodeString(envelope.Salt)
	if err != nil {
		return VaultStatus{}, err
	}
	nonce, err := base64.StdEncoding.DecodeString(envelope.Nonce)
	if err != nil {
		return VaultStatus{}, err
	}
	ciphertext, err := base64.StdEncoding.DecodeString(envelope.Data)
	if err != nil {
		return VaultStatus{}, err
	}
	key := deriveVaultKey(password, salt)
	block, err := aes.NewCipher(key)
	if err != nil {
		return VaultStatus{}, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return VaultStatus{}, err
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return VaultStatus{}, errors.New("password is incorrect or vault is corrupted")
	}
	var payload taskPayload
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return VaultStatus{}, err
	}
	if payload.Tasks == nil {
		payload.Tasks = []GUITask{}
	}
	v.key = key
	v.payload = payload
	v.unlocked = true
	return VaultStatus{Initialized: true, Unlocked: true, Path: v.path}, nil
}

func (v *taskVault) reset() (VaultStatus, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.key = nil
	v.payload = taskPayload{}
	v.unlocked = false
	if err := os.Remove(v.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return VaultStatus{}, err
	}
	return VaultStatus{Initialized: false, Unlocked: false, Path: v.path}, nil
}

func (v *taskVault) listTasks() ([]GUITask, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.unlocked {
		return nil, errors.New("vault is locked")
	}
	tasks := cloneTasks(v.payload.Tasks)
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].UpdatedAt > tasks[j].UpdatedAt
	})
	return tasks, nil
}

func (v *taskVault) getTask(id string) (GUITask, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.unlocked {
		return GUITask{}, errors.New("vault is locked")
	}
	for _, task := range v.payload.Tasks {
		if task.ID == id {
			return cloneTask(task), nil
		}
	}
	return GUITask{}, fmt.Errorf("task %s not found", id)
}

func (v *taskVault) createTask(req CreateTaskRequest) (GUITask, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.unlocked {
		return GUITask{}, errors.New("vault is locked")
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
	v.payload.Tasks = append(v.payload.Tasks, task)
	if err := v.saveLocked(nil); err != nil {
		return GUITask{}, err
	}
	return cloneTask(task), nil
}

func (v *taskVault) updateTask(req UpdateTaskRequest) (GUITask, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.unlocked {
		return GUITask{}, errors.New("vault is locked")
	}
	for index := range v.payload.Tasks {
		if v.payload.Tasks[index].ID == req.ID {
			task := &v.payload.Tasks[index]
			if strings.TrimSpace(req.Name) == "" {
				return GUITask{}, errors.New("task name is required")
			}
			task.Name = strings.TrimSpace(req.Name)
			task.Description = strings.TrimSpace(req.Description)
			task.Kind = normalizeTaskKind(req.Kind)
			task.Request = normalizeTaskRequest(req.Request, req.Kind)
			task.State.Request = task.Request
			task.UpdatedAt = time.Now().Format(time.RFC3339)
			if err := v.saveLocked(nil); err != nil {
				return GUITask{}, err
			}
			return cloneTask(*task), nil
		}
	}
	return GUITask{}, fmt.Errorf("task %s not found", req.ID)
}

func (v *taskVault) deleteTask(id string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.unlocked {
		return errors.New("vault is locked")
	}
	next := v.payload.Tasks[:0]
	deleted := false
	for _, task := range v.payload.Tasks {
		if task.ID == id {
			deleted = true
			continue
		}
		next = append(next, task)
	}
	if !deleted {
		return fmt.Errorf("task %s not found", id)
	}
	v.payload.Tasks = next
	return v.saveLocked(nil)
}

func (v *taskVault) replaceTask(task GUITask) (GUITask, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.unlocked {
		return GUITask{}, errors.New("vault is locked")
	}
	for index := range v.payload.Tasks {
		if v.payload.Tasks[index].ID == task.ID {
			task.UpdatedAt = time.Now().Format(time.RFC3339)
			v.payload.Tasks[index] = task
			if err := v.saveLocked(nil); err != nil {
				return GUITask{}, err
			}
			return cloneTask(task), nil
		}
	}
	return GUITask{}, fmt.Errorf("task %s not found", task.ID)
}

func (v *taskVault) saveLocked(existingSalt []byte) error {
	if !v.unlocked || len(v.key) == 0 {
		return errors.New("vault is locked")
	}
	salt := existingSalt
	if len(salt) == 0 {
		raw, err := os.ReadFile(v.path)
		if err == nil {
			var envelope vaultEnvelope
			if json.Unmarshal(raw, &envelope) == nil {
				salt, _ = base64.StdEncoding.DecodeString(envelope.Salt)
			}
		}
	}
	if len(salt) == 0 {
		var err error
		salt, err = randomBytes(16)
		if err != nil {
			return err
		}
	}
	plaintext, err := json.MarshalIndent(v.payload, "", "  ")
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(v.key)
	if err != nil {
		return err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce, err := randomBytes(aead.NonceSize())
	if err != nil {
		return err
	}
	envelope := vaultEnvelope{
		Version: vaultVersion,
		KDF:     kdfParams{Name: "argon2id", Time: kdfTime, Memory: kdfMemory, Threads: kdfThreads, KeyLen: kdfKeyLen},
		Salt:    base64.StdEncoding.EncodeToString(salt),
		Nonce:   base64.StdEncoding.EncodeToString(nonce),
		Data:    base64.StdEncoding.EncodeToString(aead.Seal(nil, nonce, plaintext, nil)),
	}
	out, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(v.path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(v.path, out, 0o600)
}

func deriveVaultKey(password string, salt []byte) []byte {
	return argon2.IDKey([]byte(password), salt, kdfTime, kdfMemory, kdfThreads, kdfKeyLen)
}

func randomBytes(size int) ([]byte, error) {
	out := make([]byte, size)
	_, err := rand.Read(out)
	return out, err
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
