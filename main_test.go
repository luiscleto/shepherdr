package main

import (
	"bytes"
	"runtime/debug"
	"strings"
	"testing"
)

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
