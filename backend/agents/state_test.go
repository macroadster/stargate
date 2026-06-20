package agents

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileState_CreateAndSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test_state.json")

	fs := NewFileState(path)
	if fs == nil {
		t.Fatal("NewFileState returned nil")
	}

	fs.PutSet("seen_set", map[string]bool{"a": true, "b": true})
	fs.PutMap("rejection_cache", map[string]string{"x": "reason1"})
	if err := fs.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	_, err := os.Stat(path)
	if err != nil {
		t.Fatalf("state file not created: %v", err)
	}
}

func TestFileState_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "persist.json")

	fs := NewFileState(path)
	fs.PutSet("seen", map[string]bool{"id1": true, "id2": true})
	fs.PutMap("cache", map[string]string{"k1": "v1", "k2": "v2"})
	if err := fs.Save(); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	fs2 := NewFileState(path)
	seen := fs2.GetSet("seen")
	if !seen["id1"] || !seen["id2"] {
		t.Errorf("GetSet after reload: got %v, want id1+id2", seen)
	}
	cache := fs2.GetMap("cache")
	if cache["k1"] != "v1" || cache["k2"] != "v2" {
		t.Errorf("GetMap after reload: got %v, want k1=v1,k2=v2", cache)
	}
}

func TestFileState_EmptyPath(t *testing.T) {
	fs := NewFileState("")
	if fs == nil {
		t.Fatal("NewFileState with empty path returned nil")
	}
	fs.PutSet("s", map[string]bool{"a": true})
	if err := fs.Save(); err != nil {
		t.Errorf("Save with empty path should be no-op, got: %v", err)
	}
}

func TestFileState_NonexistentFile(t *testing.T) {
	fs := NewFileState("/nonexistent/path/state.json")
	got := fs.GetSet("anything")
	if len(got) != 0 {
		t.Errorf("expected empty set for nonexistent file, got %v", got)
	}
}

func TestFileState_PutSetGetSet(t *testing.T) {
	fs := NewFileState("")
	fs.PutSet("myset", map[string]bool{"one": true, "two": true})

	got := fs.GetSet("myset")
	if len(got) != 2 || !got["one"] || !got["two"] {
		t.Errorf("PutSet/GetSet roundtrip failed: got %v", got)
	}

	empty := fs.GetSet("nonexistent")
	if len(empty) != 0 {
		t.Errorf("expected empty for nonexistent key, got %v", empty)
	}
}

func TestFileState_PutMapGetMap(t *testing.T) {
	fs := NewFileState("")
	fs.PutMap("mymap", map[string]string{"a": "1", "b": "2"})

	got := fs.GetMap("mymap")
	if got["a"] != "1" || got["b"] != "2" {
		t.Errorf("PutMap/GetMap roundtrip failed: got %v", got)
	}

	empty := fs.GetMap("nonexistent")
	if len(empty) != 0 {
		t.Errorf("expected empty for nonexistent key, got %v", empty)
	}
}

func TestFileState_Overwrite(t *testing.T) {
	fs := NewFileState("")
	fs.PutSet("key", map[string]bool{"old": true})
	fs.PutSet("key", map[string]bool{"new": true})

	got := fs.GetSet("key")
	if got["old"] {
		t.Error("expected old value to be gone after overwrite")
	}
	if !got["new"] {
		t.Error("expected new value present after overwrite")
	}
}

func TestFileState_ConcurrentSafe(t *testing.T) {
	fs := NewFileState("")
	done := make(chan bool)

	go func() {
		for i := 0; i < 50; i++ {
			fs.PutSet("s", map[string]bool{"a": true})
			_ = fs.GetSet("s")
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 50; i++ {
			fs.PutMap("m", map[string]string{"k": "v"})
			_ = fs.GetMap("m")
		}
		done <- true
	}()

	<-done
	<-done
}

func TestFileState_LoadCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.json")
	_ = os.WriteFile(path, []byte("{not json"), 0644)

	fs := NewFileState(path)
	if fs == nil {
		t.Fatal("NewFileState returned nil on corrupt file")
	}
}
