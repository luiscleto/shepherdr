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
	"time"

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

// Each Herdr keeps its own upload records and observes only its own reads, so
// closing a space or workspace on one side never removes the other side's
// files. Other terminals keeps its files while Home lists the group. Every
// case stages with one manager and observes with a new one, as after a
// Shepherdr restart.
func TestEachHerdrKeepsItsOwnFileRecords(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tuiOpen := herdr.Snapshot{Workspaces: []herdr.WorkspaceInfo{{WorkspaceID: "w1"}}}
	tuiClosed := herdr.Snapshot{Workspaces: []herdr.WorkspaceInfo{{WorkspaceID: "w2"}}}
	spacePane := herdr.PaneInfo{WorkspaceID: "sp1", PaneID: "t1", TabID: "tb1", TerminalID: "c:t1"}
	otherPane := herdr.PaneInfo{WorkspaceID: herdr.OtherTerminalsID, PaneID: "t2", TabID: herdr.OtherTerminalsID, TerminalID: "c:t2"}
	coreBoth := herdr.Snapshot{Workspaces: []herdr.WorkspaceInfo{{WorkspaceID: "sp1"}}, Panes: []herdr.PaneInfo{spacePane, otherPane}}
	coreNoSpace := herdr.Snapshot{Workspaces: []herdr.WorkspaceInfo{}, Panes: []herdr.PaneInfo{otherPane}}
	coreNoOther := herdr.Snapshot{Workspaces: []herdr.WorkspaceInfo{{WorkspaceID: "sp1"}}, Panes: []herdr.PaneInfo{spacePane}}
	for _, test := range []struct {
		name  string
		reads func(tui, core *recordingProjector)
		kept  map[string]bool
	}{
		{name: "both open, through core restarts", reads: func(tui, core *recordingProjector) {
			core.publish(coreBoth, true)
			tui.publish(tuiOpen, true)
			core.publish(coreBoth, true)
			core.publish(coreBoth, false)
		}, kept: map[string]bool{"tui:w1": true, "core:sp1": true, "core:other": true}},
		{name: "the core space closes", reads: func(tui, core *recordingProjector) {
			tui.publish(tuiOpen, true)
			core.publish(coreBoth, true)
			core.publish(coreNoSpace, false)
		}, kept: map[string]bool{"tui:w1": true, "core:sp1": false, "core:other": true}},
		{name: "the Herdr TUI workspace closes", reads: func(tui, core *recordingProjector) {
			core.publish(coreBoth, true)
			tui.publish(tuiOpen, true)
			tui.publish(tuiClosed, false)
		}, kept: map[string]bool{"tui:w1": false, "core:sp1": true, "core:other": true}},
		{name: "Other terminals is gone", reads: func(tui, core *recordingProjector) {
			tui.publish(tuiOpen, true)
			core.publish(coreBoth, true)
			core.publish(coreNoOther, false)
		}, kept: map[string]bool{"tui:w1": true, "core:sp1": true, "core:other": false}},
	} {
		t.Run(test.name, func(t *testing.T) {
			base := t.TempDir()
			if err := os.Mkdir(filepath.Join(base, "state"), 0o700); err != nil {
				t.Fatal(err)
			}
			uploadParent := filepath.Join(base, "uploads")
			statePaths := map[string]string{
				"tui": filepath.Join(base, "state", "uploads.json"), "core": filepath.Join(base, "state", "uploads-core.json"),
			}
			staged := map[string]string{}
			for side, workspaces := range map[string][]string{"tui": {"w1"}, "core": {"sp1", herdr.OtherTerminalsID}} {
				earlier, err := uploads.Open(uploadParent, statePaths[side], logger)
				if err != nil {
					t.Fatal(err)
				}
				for _, workspace := range workspaces {
					_, err = earlier.StageAndForward(context.Background(), workspace, "label", []uploads.File{{Data: strings.NewReader("notes"), Name: "notes.txt", Size: 5}},
						func() error { return nil }, func(paths []string, _ func() error) (uploads.ForwardResult, error) {
							staged[side+":"+workspace] = paths[0]
							return uploads.Forwarded, nil
						})
					if err != nil {
						t.Fatal(err)
					}
				}
				earlier.Close()
			}

			managers := map[string]*uploads.Manager{}
			for side, path := range statePaths {
				manager, err := uploads.Open(uploadParent, path, logger)
				if err != nil {
					t.Fatal(err)
				}
				managers[side] = manager
			}
			tui, core := &recordingProjector{}, &recordingProjector{}
			observeHerdrs(tui, core, notifications.NewManager(notifications.UnavailableStore(errors.New("not needed")), logger), managers["tui"], managers["core"])
			test.reads(tui, core)
			for _, manager := range managers {
				manager.Close()
			}
			for key, wantKept := range test.kept {
				_, statErr := os.Stat(staged[key])
				if kept := statErr == nil; kept != wantKept {
					t.Errorf("%s staged file kept = %v (%v), want %v", key, kept, statErr, wantKept)
				}
				side, workspace, _ := strings.Cut(key, ":")
				records, err := os.ReadFile(statePaths[side])
				if err != nil {
					t.Fatal(err)
				}
				if recorded := strings.Contains(string(records), `"`+workspace+`"`); recorded != wantKept {
					t.Errorf("%s record kept = %v, want %v: %s", key, recorded, wantKept, records)
				}
			}
		})
	}
}

func TestBodyAndReplyLimitFlags(t *testing.T) {
	if rate, err := parseUploadMinRate("8KiB"); err != nil || rate != 8<<10 {
		t.Fatalf("upload min rate 8KiB = %d, %v", rate, err)
	}
	if rate, err := parseUploadMinRate("none"); err != nil || rate != 0 {
		t.Fatalf("upload min rate none = %d, %v", rate, err)
	}
	for _, value := range []string{"", "0", "-1", "8KB", "8 KiB", "fast"} {
		if _, err := parseUploadMinRate(value); err == nil || !strings.HasPrefix(err.Error(), "-upload-min-rate ") {
			t.Fatalf("upload min rate %q error = %v", value, err)
		}
	}
	if duration, err := parseRequestBodyTime("2m"); err != nil || duration != 2*time.Minute {
		t.Fatalf("request body time 2m = %v, %v", duration, err)
	}
	for _, value := range []string{"", "0", "30", "500ms", "-5s", "none"} {
		if _, err := parseRequestBodyTime(value); err == nil || !strings.HasPrefix(err.Error(), "-request-body-time ") {
			t.Fatalf("request body time %q error = %v", value, err)
		}
	}
	if limit, err := parseHerdrReplyLimit("64MiB"); err != nil || limit != herdr.DefaultReplyLimit {
		t.Fatalf("Herdr reply limit 64MiB = %d, %v", limit, err)
	}
	for _, value := range []string{"", "0", "1023KiB", "none", "64MB"} {
		if _, err := parseHerdrReplyLimit(value); err == nil || !strings.HasPrefix(err.Error(), "-herdr-reply-limit ") {
			t.Fatalf("Herdr reply limit %q error = %v", value, err)
		}
	}
}
