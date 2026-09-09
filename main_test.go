package main

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
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
