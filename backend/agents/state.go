package agents

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

// FileState provides simple JSON file-backed persistence for seen sets etc.
// It mirrors the approach used by the original Python agents (worker_state_*.json etc).
type FileState struct {
	mu       sync.Mutex
	path     string
	data     map[string]interface{}
	loadedAt time.Time
}

func NewFileState(path string) *FileState {
	fs := &FileState{
		path: path,
		data: make(map[string]interface{}),
	}
	_ = fs.Load()
	return fs
}

func (fs *FileState) Load() error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	if fs.path == "" {
		return nil
	}
	b, err := os.ReadFile(fs.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	fs.data = m
	fs.loadedAt = time.Now()
	return nil
}

func (fs *FileState) Save() error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	if fs.path == "" {
		return nil
	}
	b, err := json.MarshalIndent(fs.data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(fs.path, b, 0644)
}

func (fs *FileState) GetSet(key string) map[string]bool {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	out := make(map[string]bool)
	if raw, ok := fs.data[key]; ok {
		if arr, ok := raw.([]interface{}); ok {
			for _, v := range arr {
				if s, ok := v.(string); ok {
					out[s] = true
				}
			}
		}
	}
	return out
}

func (fs *FileState) PutSet(key string, values map[string]bool) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	arr := make([]string, 0, len(values))
	for k := range values {
		arr = append(arr, k)
	}
	fs.data[key] = arr
}

func (fs *FileState) GetMap(key string) map[string]string {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	out := make(map[string]string)
	if raw, ok := fs.data[key]; ok {
		if m, ok := raw.(map[string]interface{}); ok {
			for k, v := range m {
				if s, ok := v.(string); ok {
					out[k] = s
				}
			}
		}
	}
	return out
}

func (fs *FileState) PutMap(key string, m map[string]string) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.data[key] = m
}
