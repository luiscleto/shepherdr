package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/luiscleto/shepherdr/internal/herdr"
	"github.com/luiscleto/shepherdr/internal/notifications"
	"github.com/luiscleto/shepherdr/internal/uploads"
)

func TestDefaultHerdrSocket(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	// Ambient Herdr targeting must not select another session for Shepherdr.
	t.Setenv("HERDR_SOCKET_PATH", filepath.Join(home, "other.sock"))
	t.Setenv("HERDR_SESSION", "other")
	for _, test := range []struct {
		name string
		xdg  string
		want string
	}{
		{name: "unset XDG", want: filepath.Join(home, ".config", "herdr", "herdr.sock")},
		{name: "empty XDG", want: filepath.Join(home, ".config", "herdr", "herdr.sock")},
		{name: "custom XDG", xdg: filepath.Join(home, "custom config"), want: filepath.Join(home, "custom config", "herdr", "herdr.sock")},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", test.xdg)
			if test.name == "unset XDG" {
				if err := os.Unsetenv("XDG_CONFIG_HOME"); err != nil {
					t.Fatal(err)
				}
			}
			// No socket or configuration directory exists: resolution must not
			// require a running Herdr or create application storage.
			got, err := defaultHerdrSocket()
			if err != nil || got != test.want {
				t.Fatalf("defaultHerdrSocket() = %q, %v; want %q, nil", got, err, test.want)
			}
		})
	}
	entries, err := os.ReadDir(home)
	if err != nil || len(entries) != 0 {
		t.Fatalf("socket resolution changed home directory: %v, %v", entries, err)
	}
}

func TestParseLogLevel(t *testing.T) {
	for _, test := range []struct {
		value string
		want  slog.Level
	}{
		{value: "debug", want: slog.LevelDebug},
		{value: "info", want: slog.LevelInfo},
		{value: "warn", want: slog.LevelWarn},
		{value: "error", want: slog.LevelError},
	} {
		t.Run(test.value, func(t *testing.T) {
			got, err := parseLogLevel(test.value)
			if err != nil || got != test.want {
				t.Fatalf("parseLogLevel(%q) = %v, %v; want %v, nil", test.value, got, err, test.want)
			}
		})
	}

	for _, value := range []string{"", "DEBUG", "warning", "trace", " info"} {
		t.Run("invalid "+value, func(t *testing.T) {
			if _, err := parseLogLevel(value); err == nil || err.Error() != "-log-level must be debug, info, warn, or error" {
				t.Fatalf("parseLogLevel(%q) error = %v", value, err)
			}
		})
	}
}

func TestBuildVersionSelection(t *testing.T) {
	moduleBuild := &debug.BuildInfo{Main: debug.Module{Version: "v0.1.0"}}
	developmentBuild := &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}
	localVCSBuild := &debug.BuildInfo{
		Main:     debug.Module{Version: "v0.0.0-20260824143153-c167bb5c7f4c"},
		Settings: []debug.BuildSetting{{Key: "vcs", Value: "git"}},
	}

	for _, test := range []struct {
		name          string
		linkerVersion string
		information   *debug.BuildInfo
		want          string
	}{
		{name: "release linker value takes precedence", linkerVersion: "v0.1.0", information: &debug.BuildInfo{Main: debug.Module{Version: "v9.9.9"}}, want: "v0.1.0"},
		{name: "go install module version", information: moduleBuild, want: "v0.1.0"},
		{name: "unversioned local build", information: developmentBuild, want: "devel"},
		{name: "local VCS pseudo-version", information: localVCSBuild, want: "devel"},
		{name: "missing build information", want: "devel"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := buildVersion(test.linkerVersion, test.information); got != test.want {
				t.Fatalf("buildVersion() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestInvitationPrintUsesOrdinaryFirstStartAndLocalInviteInstructions(t *testing.T) {
	const link = "https://shepherdr.private/#trust=invitation"
	var output bytes.Buffer
	printInvitation(&output, link)

	wantPrefix := "Trust this device\n" +
		"Open this link on that computer, or scan the QR code on a phone.\n" +
		link + "\n"
	if !strings.HasPrefix(output.String(), wantPrefix) {
		t.Fatalf("invitation output prefix = %q, want %q", output.String(), wantPrefix)
	}
}

func TestTrustedSignInLabelKeepsLegacyFallbackHonest(t *testing.T) {
	if got := trustedSignInLabel("  Personal phone  "); got != "Personal phone" {
		t.Fatalf("human label = %q", got)
	}
	if got := trustedSignInLabel("   "); got != "Trusted sign-in" {
		t.Fatalf("legacy fallback = %q", got)
	}
}

func TestHerdrCoreSocketPath(t *testing.T) {
	set := func(value string) optionalStringFlag { return optionalStringFlag{set: true, value: value} }
	for _, test := range []struct {
		name       string
		coreSocket optionalStringFlag
		runtime    string
		want       string
		err        string
	}{
		{name: "default socket", runtime: "/run/user/1", want: "/run/user/1/herdr-core/default.sock"},
		{name: "chosen socket", coreSocket: set("/tmp/core/w1.sock"), runtime: "/run/user/1", want: "/tmp/core/w1.sock"},
		{name: "chosen socket without runtime folder", coreSocket: set("/tmp/core/w1.sock"), want: "/tmp/core/w1.sock"},
		{name: "not followed without runtime folder", want: ""},
		{name: "relative socket", coreSocket: set("core.sock"), runtime: "/run/user/1", err: "-herdr-core-socket must be an absolute path"},
		{name: "empty socket", coreSocket: set(""), runtime: "/run/user/1", err: "-herdr-core-socket must be an absolute path"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := herdrCoreSocketPath(test.coreSocket, test.runtime)
			if test.err != "" {
				if err == nil || err.Error() != test.err {
					t.Fatalf("error = %v, want %q", err, test.err)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("socket = %q, %v; want %q", got, err, test.want)
			}
		})
	}
}

type recordingProjector struct {
	observer  herdr.SnapshotObserver
	published herdr.PublishedSnapshotObserver
}

func (p *recordingProjector) SetSnapshotObserver(observer herdr.SnapshotObserver) {
	p.observer = observer
}

func (p *recordingProjector) SetPublishedSnapshotObserver(observer herdr.PublishedSnapshotObserver) {
	p.published = observer
}

func (p *recordingProjector) publish(snapshot herdr.Snapshot, baseline bool) {
	if p.observer != nil {
		p.observer.ObserveSnapshot(snapshot, baseline)
	}
	if p.published != nil {
		p.published.ObservePublishedSnapshot(snapshot, baseline)
	}
}

// Upload records hold Herdr TUI workspace ids. herdr-core reads, whose spaces
// have other ids, run beside Herdr TUI reads and must never remove files staged
// for a Herdr TUI workspace; Herdr TUI reads still clean up closed workspaces.
func TestHerdrCoreReadsKeepFilesStagedForHerdrTUI(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	coreRead := herdr.Snapshot{Workspaces: []herdr.WorkspaceInfo{{WorkspaceID: "sp1"}}}
	tuiOpen := herdr.Snapshot{Workspaces: []herdr.WorkspaceInfo{{WorkspaceID: "w1"}}}
	tuiClosed := herdr.Snapshot{Workspaces: []herdr.WorkspaceInfo{{WorkspaceID: "w2"}}}
	for _, test := range []struct {
		name     string
		reads    func(tui, core *recordingProjector)
		wantKept bool
	}{
		{name: "herdr-core reads, restarts and an empty core", reads: func(tui, core *recordingProjector) {
			core.publish(coreRead, true)
			core.publish(coreRead, false)
			core.publish(coreRead, true)
			core.publish(herdr.Snapshot{}, false)
		}, wantKept: true},
		{name: "both, with the workspace open", reads: func(tui, core *recordingProjector) {
			tui.publish(tuiOpen, true)
			core.publish(coreRead, true)
			tui.publish(tuiOpen, false)
			core.publish(herdr.Snapshot{}, false)
		}, wantKept: true},
		{name: "both, after the workspace closed", reads: func(tui, core *recordingProjector) {
			core.publish(coreRead, true)
			tui.publish(tuiClosed, true)
		}, wantKept: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			base := t.TempDir()
			if err := os.Mkdir(filepath.Join(base, "state"), 0o700); err != nil {
				t.Fatal(err)
			}
			uploadParent, statePath := filepath.Join(base, "uploads"), filepath.Join(base, "state", "uploads.json")
			earlier, err := uploads.Open(uploadParent, statePath, logger)
			if err != nil {
				t.Fatal(err)
			}
			var staged string
			_, err = earlier.StageAndForward(context.Background(), "w1", "alpha", []uploads.File{{Name: "notes.txt", Data: []byte("notes")}},
				func() error { return nil }, func(paths []string, _ func() error) (uploads.ForwardResult, error) {
					staged = paths[0]
					return uploads.Forwarded, nil
				})
			earlier.Close()
			if err != nil || staged == "" {
				t.Fatalf("stage = %q, %v", staged, err)
			}

			manager, err := uploads.Open(uploadParent, statePath, logger)
			if err != nil {
				t.Fatal(err)
			}
			tui, core := &recordingProjector{}, &recordingProjector{}
			observeHerdrs(tui, core, notifications.NewManager(notifications.UnavailableStore(errors.New("not needed")), logger), manager)
			test.reads(tui, core)
			manager.Close()
			_, statErr := os.Stat(staged)
			if kept := statErr == nil; kept != test.wantKept {
				t.Fatalf("staged file kept = %v (%v), want %v", kept, statErr, test.wantKept)
			}
			records, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			if recorded := strings.Contains(string(records), `"w1"`); recorded != test.wantKept {
				t.Fatalf("record for w1 kept = %v, want %v: %s", recorded, test.wantKept, records)
			}
		})
	}
}
