package main

import (
	"bytes"
	"strings"
	"testing"
)

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
