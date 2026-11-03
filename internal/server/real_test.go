package server

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/luiscleto/shepherdr/internal/herdr"
)

func TestRealProductionHome(t *testing.T) {
	base := os.Getenv("SHEPHERDR_REAL_URL")
	if base == "" {
		t.Skip("set SHEPHERDR_REAL_URL for the bounded production-path check")
	}
	homeURL := websocketTestURL(t, base, "/api/home")
	homeSocket, _, err := websocket.DefaultDialer.Dial(homeURL.String(), http.Header{"Origin": {base}})
	if err != nil {
		t.Fatal(err)
	}
	defer homeSocket.Close()

	// With an expected Herdr Core version set, Home must also report the Core
	// side Live with its Home at that version before the read deadline.
	expectCoreVersion := os.Getenv("SHEPHERDR_REAL_EXPECT_CORE_VERSION")
	deadline := 8 * time.Second
	if expectCoreVersion != "" {
		deadline = 20 * time.Second
	}
	_ = homeSocket.SetReadDeadline(time.Now().Add(deadline))
	var homeMessage struct {
		Epoch string      `json:"epoch"`
		TUI   herdr.State `json:"tui"`
		Core  herdr.State `json:"core"`
	}
	ready := func() bool {
		if homeMessage.TUI.Connection != herdr.ConnectionLive {
			return false
		}
		if expectCoreVersion == "" {
			return true
		}
		core := homeMessage.Core
		return core.Connection == herdr.ConnectionLive && core.HasHome && core.HerdrVersion == expectCoreVersion
	}
	for !ready() {
		if err := homeSocket.ReadJSON(&homeMessage); err != nil {
			if expectCoreVersion != "" {
				t.Fatalf("Home did not report the Herdr TUI side Live and the Herdr Core side Live with Home at version %q; last saw TUI %s, Core %s, has Home %t, version %q: %v", expectCoreVersion, homeMessage.TUI.Connection, homeMessage.Core.Connection, homeMessage.Core.HasHome, homeMessage.Core.HerdrVersion, err)
			}
			t.Fatal(err)
		}
	}
	state := homeMessage.TUI
	if homeMessage.Epoch == "" {
		t.Fatal("Home publication did not identify its server epoch")
	}
	terminalCount := 0
	var countWorkspace func(herdr.Workspace)
	countWorkspace = func(workspace herdr.Workspace) {
		for _, tab := range workspace.Tabs {
			terminalCount += len(tab.Terminals)
		}
		for _, worktree := range workspace.Worktrees {
			countWorkspace(worktree)
		}
	}
	for _, workspace := range state.Home.Workspaces {
		countWorkspace(workspace)
	}
	t.Logf("Herdr Core side: %s, has Home %t, version %q", homeMessage.Core.Connection, homeMessage.Core.HasHome, homeMessage.Core.HerdrVersion)
	t.Logf("Herdr TUI Home is live: %d workspaces, %d terminals, %d working, %d blocked", len(state.Home.Workspaces), terminalCount, state.Home.WorkingCount, state.Home.BlockedCount)
}

func websocketTestURL(t *testing.T, base, endpoint string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(strings.TrimRight(base, "/") + endpoint)
	if err != nil {
		t.Fatal(err)
	}
	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	}
	return parsed
}
