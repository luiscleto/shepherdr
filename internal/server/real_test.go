package server

import (
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/luisc/shepherdr/internal/herdr"
)

func TestRealProductionHome(t *testing.T) {
	base := os.Getenv("SHEPHERDR_REAL_URL")
	if base == "" {
		t.Skip("set SHEPHERDR_REAL_URL for the bounded production-path check")
	}
	homeURL := websocketTestURL(t, base, "/api/home")
	homeSocket, _, err := websocket.DefaultDialer.Dial(homeURL.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer homeSocket.Close()
	_ = homeSocket.SetReadDeadline(time.Now().Add(8 * time.Second))

	var homeMessage struct {
		herdr.State
		Epoch string `json:"epoch"`
	}
	state := homeMessage.State
	for state.Connection != herdr.ConnectionLive {
		if err := homeSocket.ReadJSON(&homeMessage); err != nil {
			t.Fatal(err)
		}
		state = homeMessage.State
	}
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
	t.Logf("Home is live: %d workspaces, %d terminals, %d working, %d blocked", len(state.Home.Workspaces), terminalCount, state.Home.WorkingCount, state.Home.BlockedCount)
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
