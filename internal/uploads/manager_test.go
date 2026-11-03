package uploads

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luiscleto/shepherdr/internal/herdr"
)

func memoryFile(name, data string) File {
	return File{Data: strings.NewReader(data), Name: name, Size: int64(len(data))}
}

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
		memoryFile(`C:\\fakepath\\diagram.svg`, "first"),
		memoryFile("../diagram.svg", "second"),
		memoryFile("bad\nname.txt", "third"),
	}, func() error { return nil }, func(paths []string, _ func() error) (ForwardResult, error) {
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
	_, err = manager.StageAndForward(context.Background(), "workspace-1", "Renamed", []File{memoryFile("notes", "ok")},
		func() error { return nil }, func(paths []string, _ func() error) (ForwardResult, error) {
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
	_, err := manager.StageAndForward(context.Background(), "workspace-1", "One", []File{memoryFile("one.txt", "one")},
		func() error { return nil }, func(paths []string, _ func() error) (ForwardResult, error) {
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
	_, err = manager.StageAndForward(context.Background(), "workspace-1", "One", []File{memoryFile("two.txt", "two")},
		func() error { return nil }, func(paths []string, _ func() error) (ForwardResult, error) {
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
	_, err := manager.StageAndForward(context.Background(), "workspace-1", "One", []File{memoryFile("keep.txt", "keep")},
		func() error { return nil }, func(paths []string, _ func() error) (ForwardResult, error) {
			firstPath = paths[0]
			return Forwarded, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	var rolledBack string
	result, err := manager.StageAndForward(context.Background(), "workspace-1", "One", []File{memoryFile("remove.txt", "remove")},
		func() error { return nil }, func(paths []string, _ func() error) (ForwardResult, error) {
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

func TestLatePathSwapIsRefusedAndRollbackStaysOnVerifiedHandle(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "uploads")
	stateDirectory := filepath.Join(base, "state")
	if err := os.Mkdir(stateDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	manager, err := Open(root, filepath.Join(stateDirectory, "uploads.json"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	movedRoot := filepath.Join(base, "uploads-moved")
	var originalPath, decoyPath string
	result, sendErr := manager.StageAndForward(context.Background(), "workspace-1", "One", []File{memoryFile("swap.txt", "original")},
		func() error { return nil }, func(paths []string, validateStaged func() error) (ForwardResult, error) {
			originalPath = filepath.Join(movedRoot, filepath.Base(filepath.Dir(paths[0])), filepath.Base(paths[0]))
			if err := os.Rename(root, movedRoot); err != nil {
				t.Fatal(err)
			}
			replacementDirectory := filepath.Join(root, filepath.Base(filepath.Dir(paths[0])))
			if err := os.MkdirAll(replacementDirectory, 0o700); err != nil {
				t.Fatal(err)
			}
			decoyPath = filepath.Join(replacementDirectory, filepath.Base(paths[0]))
			if err := os.WriteFile(decoyPath, []byte("decoy"), 0o600); err != nil {
				t.Fatal(err)
			}
			validationErr := validateStaged()
			if validationErr == nil {
				t.Fatal("redirected absolute upload path passed late validation")
			}
			return NotSent, validationErr
		})
	if result != NotSent || sendErr == nil {
		t.Fatalf("swapped path result = %s, %v", result, sendErr)
	}
	if _, err := os.Stat(originalPath); !os.IsNotExist(err) {
		t.Fatalf("request file remained in the verified directory: %v", err)
	}
	if data, err := os.ReadFile(decoyPath); err != nil || string(data) != "decoy" {
		t.Fatalf("rollback followed the replacement path: %q, %v", data, err)
	}
}

func TestCleanupStaysOnVerifiedParentAfterConfiguredPathSwap(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "uploads")
	stateDirectory := filepath.Join(base, "state")
	if err := os.Mkdir(stateDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	manager, err := Open(root, filepath.Join(stateDirectory, "uploads.json"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	var directory string
	_, err = manager.StageAndForward(context.Background(), "workspace-1", "One", []File{memoryFile("keep.txt", "keep")},
		func() error { return nil }, func(paths []string, _ func() error) (ForwardResult, error) {
			directory = filepath.Dir(paths[0])
			return Forwarded, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	movedRoot := filepath.Join(base, "uploads-moved")
	if err := os.Rename(root, movedRoot); err != nil {
		t.Fatal(err)
	}
	replacementDirectory := filepath.Join(root, filepath.Base(directory))
	if err := os.MkdirAll(replacementDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	decoy := filepath.Join(replacementDirectory, "decoy")
	if err := os.WriteFile(decoy, []byte("decoy"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := manager.cleanupWorkspace("workspace-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(movedRoot, filepath.Base(directory))); !os.IsNotExist(err) {
		t.Fatalf("verified upload directory remains after cleanup: %v", err)
	}
	if data, err := os.ReadFile(decoy); err != nil || string(data) != "decoy" {
		t.Fatalf("cleanup followed the replacement path: %q, %v", data, err)
	}
}

func TestPartialStageFailureRemovesEarlierRequestFiles(t *testing.T) {
	manager, _ := openTestManager(t)
	result, err := manager.StageAndForward(context.Background(), "workspace-1", "One", []File{
		memoryFile("first.txt", "first"),
		memoryFile(strings.Repeat("x", 300), "second"),
	}, func() error { return nil }, func([]string, func() error) (ForwardResult, error) {
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

func TestUnsafeUnicodeIsRemovedFromNamesAndRejectedFromTerminalPaths(t *testing.T) {
	for name, character := range map[string]string{
		"bidi mark":           "\u200e",
		"bidi override":       "\u202e",
		"bidi isolate":        "\u2066",
		"byte order mark":     "\ufeff",
		"line separator":      "\u2028",
		"paragraph separator": "\u2029",
		"word joiner":         "\u2060",
		"zero width space":    "\u200b",
	} {
		if got := safeBasename("left" + character + "right.txt"); got != "leftright.txt" {
			t.Errorf("%s filename = %q", name, got)
		}
		if err := ValidatePathText("/tmp/left" + character + "right.txt"); err == nil {
			t.Errorf("%s terminal path was accepted", name)
		}
	}
	if got := safeBasename("résumé_日本語.txt"); got != "résumé_日本語.txt" {
		t.Fatalf("ordinary Unicode filename = %q", got)
	}
	for name, filename := range map[string]string{
		"zero width joiner":     "क्\u200dष.txt",
		"zero width non-joiner": "می\u200cخواهم.txt",
	} {
		if got := safeBasename(filename); got != filename {
			t.Errorf("%s filename = %q, want %q", name, got, filename)
		}
		if err := ValidatePathText(filepath.Join("/tmp", filename)); err != nil {
			t.Errorf("%s terminal path was rejected: %v", name, err)
		}
	}
}

func TestStagedNamesDropShellCharactersAndStayReadableAndUnique(t *testing.T) {
	for name, want := range map[string]string{
		"report$(curl x|sh).pdf":    "reportcurl xsh.pdf",
		"a`id`;b&c<d>e.txt":         "aidbcde.txt",
		`quote'd "name"!.txt`:       "quoted name.txt",
		"photo (1).jpg":             "photo 1.jpg",
		"résumé #2 [final] ~v1.pdf": "résumé #2 [final] ~v1.pdf",
		"$;|&":                      "file",
	} {
		if got := safeBasename(name); got != want {
			t.Errorf("safeBasename(%q) = %q, want %q", name, got, want)
		}
	}
	manager, _ := openTestManager(t)
	var paths []string
	_, err := manager.StageAndForward(context.Background(), "workspace-1", "One", []File{
		memoryFile("a$b.txt", "one"),
		memoryFile("a;b.txt", "two"),
		memoryFile("ab.txt", "three"),
	}, func() error { return nil }, func(staged []string, _ func() error) (ForwardResult, error) {
		paths = staged
		return Forwarded, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, path := range paths {
		names = append(names, filepath.Base(path))
	}
	if strings.Join(names, "|") != "ab.txt|ab (1).txt|ab (2).txt" {
		t.Fatalf("staged names = %q", names)
	}
}

func TestCleanupRefusesMismatchedMarkerAndRetainsRecord(t *testing.T) {
	manager, base := openTestManager(t)
	var directory string
	_, err := manager.StageAndForward(context.Background(), "workspace-1", "One", []File{memoryFile("one", "one")},
		func() error { return nil }, func(paths []string, _ func() error) (ForwardResult, error) {
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
		_, _ = manager.StageAndForward(context.Background(), "workspace-1", "One", []File{memoryFile("one", "one")},
			func() error { return nil }, func(paths []string, _ func() error) (ForwardResult, error) {
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
	_, err = first.StageAndForward(context.Background(), "stale", "Stale", []File{memoryFile("one", "one")},
		func() error { return nil }, func(paths []string, _ func() error) (ForwardResult, error) {
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
		_, _ = manager.StageAndForward(context.Background(), "workspace-1", "One", []File{memoryFile("one", "one")},
			func() error { return nil }, func([]string, func() error) (ForwardResult, error) {
				close(started)
				<-block
				return Forwarded, nil
			})
	}()
	<-started
	var other atomic.Bool
	_, err := manager.StageAndForward(context.Background(), "workspace-2", "Two", []File{memoryFile("two", "two")},
		func() error { return nil }, func([]string, func() error) (ForwardResult, error) {
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

func TestFileWhoseBytesEndEarlyIsRemoved(t *testing.T) {
	manager, _ := openTestManager(t)
	result, err := manager.StageAndForward(context.Background(), "workspace-1", "One", []File{
		memoryFile("first.txt", "first"),
		{Data: strings.NewReader("only part"), Name: "second.txt", Size: 1 << 20},
	}, func() error { return nil }, func([]string, func() error) (ForwardResult, error) {
		t.Fatal("a file that ended early reached terminal forwarding")
		return Forwarded, nil
	})
	if result != NotSent || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("short file result = %s, %v", result, err)
	}
	entries, err := os.ReadDir(manager.gate("workspace-1").association.Directory)
	if err != nil || len(entries) != 1 || entries[0].Name() != markerName {
		t.Fatalf("partial files remain: %+v, %v", entries, err)
	}
}

func TestSendsToOneWorkspaceDoNotWaitForEachOther(t *testing.T) {
	manager, _ := openTestManager(t)
	reader, writer := io.Pipe()
	defer writer.Close()
	slow := make(chan error, 1)
	go func() {
		_, err := manager.StageAndForward(context.Background(), "workspace-1", "One", []File{{Data: reader, Name: "slow.bin", Size: 10}},
			func() error { return nil }, func([]string, func() error) (ForwardResult, error) { return Forwarded, nil })
		slow <- err
	}()
	if _, err := writer.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() {
		_, err := manager.StageAndForward(context.Background(), "workspace-1", "One", []File{memoryFile("quick.txt", "quick")},
			func() error { return nil }, func([]string, func() error) (ForwardResult, error) { return Forwarded, nil })
		finished <- err
	}()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a quick send waited for a slow send to the same workspace")
	}
	if _, err := writer.Write([]byte("defghij")); err != nil {
		t.Fatal(err)
	}
	if err := <-slow; err != nil {
		t.Fatal(err)
	}
}
