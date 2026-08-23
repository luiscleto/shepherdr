package uploads

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luisc/shepherdr/internal/herdr"
)

func openTestManager(t *testing.T) (*Manager, string) {
	t.Helper()
	base := t.TempDir()
	stateDirectory := filepath.Join(base, "state")
	if err := os.Mkdir(stateDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	manager, err := Open(filepath.Join(base, "uploads"), filepath.Join(stateDirectory, "uploads.json"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	return manager, stateDirectory
}

func TestStageCreatesVerifiedOwnerOnlyAssociationAndRecognizableConflictNames(t *testing.T) {
	manager, base := openTestManager(t)
	var forwardedPaths []string
	result, err := manager.StageAndForward(context.Background(), "workspace-1", "Review / work", []File{
		{Name: `C:\\fakepath\\diagram.svg`, Data: []byte("first")},
		{Name: "../diagram.svg", Data: []byte("second")},
		{Name: "bad\nname.txt", Data: []byte("third")},
	}, func() error { return nil }, func(paths []string) (ForwardResult, error) {
		forwardedPaths = append([]string(nil), paths...)
		return Forwarded, nil
	})
	if err != nil || result != Forwarded {
		t.Fatalf("stage result = %s, %v", result, err)
	}
	wantNames := []string{"diagram.svg", "diagram (1).svg", "badname.txt"}
	for index, path := range forwardedPaths {
		if got := filepath.Base(path); got != wantNames[index] {
			t.Errorf("path %d basename = %q, want %q", index, got, wantNames[index])
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil || string(data) != []string{"first", "second", "third"}[index] {
			t.Errorf("path %d bytes = %q, %v", index, data, readErr)
		}
		info, statErr := os.Stat(path)
		if statErr != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("path %d mode = %v, %v", index, infoMode(info), statErr)
		}
	}
	directory := filepath.Dir(forwardedPaths[0])
	if info, statErr := os.Stat(directory); statErr != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("directory mode = %v, %v", infoMode(info), statErr)
	}
	if info, statErr := os.Stat(filepath.Join(directory, markerName)); statErr != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("marker mode = %v, %v", infoMode(info), statErr)
	}
	if info, statErr := os.Stat(filepath.Join(base, "uploads.json")); statErr != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("state mode = %v, %v", infoMode(info), statErr)
	}

	var reused string
	_, err = manager.StageAndForward(context.Background(), "workspace-1", "Renamed", []File{{Name: "notes", Data: []byte("ok")}},
		func() error { return nil }, func(paths []string) (ForwardResult, error) {
			reused = filepath.Dir(paths[0])
			return Forwarded, nil
		})
	if err != nil || reused != directory {
		t.Fatalf("verified association was not reused: %q, %v", reused, err)
	}
}

func TestMismatchedAssociationIsNeverReusedOrDeleted(t *testing.T) {
	manager, _ := openTestManager(t)
	var original string
	_, err := manager.StageAndForward(context.Background(), "workspace-1", "One", []File{{Name: "one.txt", Data: []byte("one")}},
		func() error { return nil }, func(paths []string) (ForwardResult, error) {
			original = filepath.Dir(paths[0])
			return Forwarded, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	badMarker, _ := json.Marshal(marker{Ownership: strings.Repeat("0", 32), WorkspaceID: "workspace-1"})
	if err := os.WriteFile(filepath.Join(original, markerName), badMarker, 0o600); err != nil {
		t.Fatal(err)
	}
	var replacement string
	_, err = manager.StageAndForward(context.Background(), "workspace-1", "One", []File{{Name: "two.txt", Data: []byte("two")}},
		func() error { return nil }, func(paths []string) (ForwardResult, error) {
			replacement = filepath.Dir(paths[0])
			return Forwarded, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if replacement == original {
		t.Fatal("mismatched directory was reused")
	}
	if _, err := os.Stat(original); err != nil {
		t.Fatalf("mismatched directory was removed: %v", err)
	}
}

func TestDefiniteFailureRollsBackOnlyThisRequest(t *testing.T) {
	manager, _ := openTestManager(t)
	var firstPath string
	_, err := manager.StageAndForward(context.Background(), "workspace-1", "One", []File{{Name: "keep.txt", Data: []byte("keep")}},
		func() error { return nil }, func(paths []string) (ForwardResult, error) {
			firstPath = paths[0]
			return Forwarded, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	var rolledBack string
	result, err := manager.StageAndForward(context.Background(), "workspace-1", "One", []File{{Name: "remove.txt", Data: []byte("remove")}},
		func() error { return nil }, func(paths []string) (ForwardResult, error) {
			rolledBack = paths[0]
			return NotSent, context.Canceled
		})
	if result != NotSent || err == nil {
		t.Fatalf("definite result = %s, %v", result, err)
	}
	if _, err := os.Stat(rolledBack); !os.IsNotExist(err) {
		t.Fatalf("request file remains after rollback: %v", err)
	}
	if data, err := os.ReadFile(firstPath); err != nil || string(data) != "keep" {
		t.Fatalf("earlier file changed during rollback: %q, %v", data, err)
	}
}

func TestPartialStageFailureRemovesEarlierRequestFiles(t *testing.T) {
	manager, _ := openTestManager(t)
	result, err := manager.StageAndForward(context.Background(), "workspace-1", "One", []File{
		{Name: "first.txt", Data: []byte("first")},
		{Name: strings.Repeat("x", 300), Data: []byte("second")},
	}, func() error { return nil }, func([]string) (ForwardResult, error) {
		t.Fatal("partial stage failure reached terminal forwarding")
		return Forwarded, nil
	})
	if result != NotSent || err == nil {
		t.Fatalf("partial stage result = %s, %v", result, err)
	}
	record := manager.gate("workspace-1").association
	if record == nil {
		t.Fatal("workspace association was not retained")
	}
	entries, err := os.ReadDir(record.Directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != markerName {
		t.Fatalf("partial request files remain: %+v", entries)
	}
}

func TestCleanupRefusesMismatchedMarkerAndRetainsRecord(t *testing.T) {
	manager, base := openTestManager(t)
	var directory string
	_, err := manager.StageAndForward(context.Background(), "workspace-1", "One", []File{{Name: "one", Data: []byte("one")}},
		func() error { return nil }, func(paths []string) (ForwardResult, error) {
			directory = filepath.Dir(paths[0])
			return Forwarded, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, markerName), []byte(`{"ownership":"00000000000000000000000000000000","workspace_id":"workspace-1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	manager.ObservePublishedSnapshot(herdr.Snapshot{}, true)
	manager.Close()
	if _, err := os.Stat(directory); err != nil {
		t.Fatalf("mismatched directory was deleted: %v", err)
	}
	records, err := readAssociations(filepath.Join(base, "uploads.json"))
	if err != nil || records["workspace-1"].Directory != directory {
		t.Fatalf("failed cleanup record = %+v, %v", records, err)
	}
}

func TestStableWorkspaceGateSerializesSendAndPostPublicationCleanup(t *testing.T) {
	manager, _ := openTestManager(t)
	if manager.gate("workspace-1") != manager.gate("workspace-1") {
		t.Fatal("manager replaced a workspace gate")
	}
	forwarding := make(chan string, 1)
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = manager.StageAndForward(context.Background(), "workspace-1", "One", []File{{Name: "one", Data: []byte("one")}},
			func() error { return nil }, func(paths []string) (ForwardResult, error) {
				forwarding <- filepath.Dir(paths[0])
				<-release
				return Forwarded, nil
			})
	}()
	directory := <-forwarding
	manager.ObservePublishedSnapshot(herdr.Snapshot{Workspaces: []herdr.WorkspaceInfo{{WorkspaceID: "workspace-1"}}}, true)
	manager.ObservePublishedSnapshot(herdr.Snapshot{Workspaces: []herdr.WorkspaceInfo{}}, false)
	time.Sleep(20 * time.Millisecond)
	if _, err := os.Stat(directory); err != nil {
		t.Fatalf("cleanup did not wait for active send: %v", err)
	}
	close(release)
	<-done
	manager.Close()
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatalf("owned directory remains after cleanup: %v", err)
	}
}

func TestStartupReconciliationCleansOnlyMissingVerifiedWorkspace(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "uploads")
	stateDirectory := filepath.Join(base, "state")
	if err := os.Mkdir(stateDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(stateDirectory, "uploads.json")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	first, err := Open(root, state, logger)
	if err != nil {
		t.Fatal(err)
	}
	var staleDirectory string
	_, err = first.StageAndForward(context.Background(), "stale", "Stale", []File{{Name: "one", Data: []byte("one")}},
		func() error { return nil }, func(paths []string) (ForwardResult, error) {
			staleDirectory = filepath.Dir(paths[0])
			return Forwarded, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	first.Close()

	second, err := Open(root, state, logger)
	if err != nil {
		t.Fatal(err)
	}
	second.ObservePublishedSnapshot(herdr.Snapshot{Workspaces: []herdr.WorkspaceInfo{{WorkspaceID: "present"}}}, true)
	second.Close()
	if _, err := os.Stat(staleDirectory); !os.IsNotExist(err) {
		t.Fatalf("startup stale directory remains: %v", err)
	}
	records, err := readAssociations(state)
	if err != nil || len(records) != 0 {
		t.Fatalf("startup records = %+v, %v", records, err)
	}
}

func TestConcurrentDifferentWorkspaceCanPassItsOwnGate(t *testing.T) {
	manager, _ := openTestManager(t)
	block := make(chan struct{})
	started := make(chan struct{})
	go func() {
		_, _ = manager.StageAndForward(context.Background(), "workspace-1", "One", []File{{Name: "one", Data: []byte("one")}},
			func() error { return nil }, func([]string) (ForwardResult, error) {
				close(started)
				<-block
				return Forwarded, nil
			})
	}()
	<-started
	var other atomic.Bool
	_, err := manager.StageAndForward(context.Background(), "workspace-2", "Two", []File{{Name: "two", Data: []byte("two")}},
		func() error { return nil }, func([]string) (ForwardResult, error) {
			other.Store(true)
			return Forwarded, nil
		})
	close(block)
	if err != nil || !other.Load() {
		t.Fatalf("other workspace was blocked: %v", err)
	}
}

func infoMode(info os.FileInfo) os.FileMode {
	if info == nil {
		return 0
	}
	return info.Mode().Perm()
}
