package herdr

import (
	"os"
	"path/filepath"
	"testing"
)

const darwinUnixSocketPathMax = 103

func shortSocketPath(t *testing.T) string {
	t.Helper()
	directory, err := os.MkdirTemp(os.TempDir(), "shr-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(directory); err != nil {
			t.Errorf("remove short socket directory: %v", err)
		}
	})
	path := filepath.Join(directory, "h.sock")
	if len([]byte(path)) > darwinUnixSocketPathMax {
		t.Fatalf("Unix socket path is %d bytes, exceeds Darwin limit %d: %q", len([]byte(path)), darwinUnixSocketPathMax, path)
	}
	return path
}
