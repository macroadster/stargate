package tui

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestRunRefusesPipe(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = r.Close()
		_ = w.Close()
	})
	err = Run(context.Background(), NewHub(), w, r, Options{})
	if !errors.Is(err, ErrNotATTY) {
		t.Fatalf("got %v", err)
	}
}

func TestViewFromEnv(t *testing.T) {
	if ViewFromEnv("") != ViewWelcome || ViewFromEnv("nope") != ViewWelcome {
		t.Fatal("welcome")
	}
	if ViewFromEnv("monitor") != ViewMonitor || ViewFromEnv("LOGS") != ViewLogs {
		t.Fatal("views")
	}
}
