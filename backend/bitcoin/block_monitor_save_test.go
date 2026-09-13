package bitcoin

import (
	"path/filepath"
	"testing"
)

func TestIsArchivedBlockPath(t *testing.T) {
	root := "/data/blocks"
	cases := []struct {
		path string
		want bool
	}{
		{root, false},
		{filepath.Join(root, "000"), false},
		{filepath.Join(root, "000", "152", "152146_00000000"), false},
		{filepath.Join(root, "reorgs"), true},
		{filepath.Join(root, "reorgs", "000", "152", "152146_00000000"), true},
		{filepath.Join(root, "_fork_quarantine_x", "000"), true},
		{filepath.Join("/other", "reorgs"), false},
	}
	for _, tc := range cases {
		if got := IsArchivedBlockPath(root, tc.path); got != tc.want {
			t.Fatalf("IsArchivedBlockPath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}
