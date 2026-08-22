package notifications

import (
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/luisc/shepherdr/internal/herdr"
)

const (
	stateVersion       = 2
	legacyStateVersion = 1
	MaxSubscriptions   = 256
)

type EventSettings struct {
	Working         bool `json:"working"`
	Blocked         bool `json:"blocked"`
	Idle            bool `json:"idle"`
	Done            bool `json:"done"`
	Unknown         bool `json:"unknown"`
	WorkspaceOpened bool `json:"workspace_opened"`
	WorkspaceClosed bool `json:"workspace_closed"`
}

func DefaultEventSettings() EventSettings {
	return EventSettings{Blocked: true, Done: true}
}

type SubscriptionKeys struct {
	Auth   string `json:"auth"`
	P256dh string `json:"p256dh"`
}

type Subscription struct {
	Endpoint       string           `json:"endpoint"`
	ExpirationTime *float64         `json:"expiration_time,omitempty"`
	Keys           SubscriptionKeys `json:"keys"`
	Events         EventSettings    `json:"events"`
	TrustID        string           `json:"trust_id,omitempty"`
}

type BrowserSubscription struct {
	Endpoint       string           `json:"endpoint"`
	ExpirationTime *float64         `json:"expirationTime"`
	Keys           SubscriptionKeys `json:"keys"`
}

type EventKind string

const (
	EventStatus          EventKind = "status"
	EventWorkspaceOpened EventKind = "workspace_opened"
	EventWorkspaceClosed EventKind = "workspace_closed"
)

type Event struct {
	Kind          EventKind
	Status        herdr.Status
	WorkspaceName string
	PaneID        string
	TerminalID    string
	Destination   string
}

func (e Event) selected(settings EventSettings) bool {
	switch e.Kind {
	case EventWorkspaceOpened:
		return settings.WorkspaceOpened
	case EventWorkspaceClosed:
		return settings.WorkspaceClosed
	case EventStatus:
		switch e.Status {
		case herdr.StatusWorking:
			return settings.Working
		case herdr.StatusBlocked:
			return settings.Blocked
		case herdr.StatusIdle:
			return settings.Idle
		case herdr.StatusDone:
			return settings.Done
		case herdr.StatusUnknown:
			return settings.Unknown
		}
	}
	return false
}

type snapshotEvaluator struct {
	previous *herdr.Snapshot
}

func (e *snapshotEvaluator) Observe(snapshot herdr.Snapshot, baseline bool) []Event {
	previous := e.previous
	copy := snapshot
	e.previous = &copy
	if baseline || previous == nil {
		return nil
	}
	currentWorkspaces := make(map[string]herdr.WorkspaceInfo, len(snapshot.Workspaces))
	for _, workspace := range snapshot.Workspaces {
		currentWorkspaces[workspace.WorkspaceID] = workspace
	}

	previousAgents := make(map[string]herdr.AgentInfo, len(previous.Agents))
	for _, agent := range previous.Agents {
		previousAgents[terminalKey(agent.PaneID, agent.TerminalID)] = agent
	}
	events := make([]Event, 0)
	for _, agent := range snapshot.Agents {
		prior, ok := previousAgents[terminalKey(agent.PaneID, agent.TerminalID)]
		if !ok || prior.AgentStatus == agent.AgentStatus {
			continue
		}
		destination := "/#terminal=" + url.QueryEscape(agent.PaneID) + "&terminal_id=" + url.QueryEscape(agent.TerminalID)
		events = append(events, Event{
			Kind:          EventStatus,
			Status:        agent.AgentStatus,
			WorkspaceName: notificationWorkspaceName(currentWorkspaces[agent.WorkspaceID].Label),
			PaneID:        agent.PaneID,
			TerminalID:    agent.TerminalID,
			Destination:   destination,
		})
	}

	previousWorkspaces := make(map[string]herdr.WorkspaceInfo, len(previous.Workspaces))
	for _, workspace := range previous.Workspaces {
		previousWorkspaces[workspace.WorkspaceID] = workspace
	}
	opened := make([]herdr.WorkspaceInfo, 0)
	for _, workspace := range snapshot.Workspaces {
		if _, ok := previousWorkspaces[workspace.WorkspaceID]; !ok {
			opened = append(opened, workspace)
		}
	}
	closed := make([]herdr.WorkspaceInfo, 0)
	for _, workspace := range previous.Workspaces {
		if _, ok := currentWorkspaces[workspace.WorkspaceID]; !ok {
			closed = append(closed, workspace)
		}
	}
	if len(opened) > 0 {
		events = append(events, Event{
			Kind: EventWorkspaceOpened, WorkspaceName: singleWorkspaceName(opened), Destination: "/",
		})
	}
	if len(closed) > 0 {
		events = append(events, Event{
			Kind: EventWorkspaceClosed, WorkspaceName: singleWorkspaceName(closed), Destination: "/",
		})
	}
	return events
}

func singleWorkspaceName(workspaces []herdr.WorkspaceInfo) string {
	if len(workspaces) != 1 {
		return ""
	}
	return notificationWorkspaceName(workspaces[0].Label)
}

func notificationWorkspaceName(label string) string {
	name := strings.TrimSpace(label)
	if utf8.RuneCountInString(name) > 160 {
		return ""
	}
	return name
}

func terminalKey(paneID, terminalID string) string {
	return fmt.Sprintf("%d:%s%d:%s", len(paneID), paneID, len(terminalID), terminalID)
}
