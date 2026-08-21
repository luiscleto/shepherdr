package notifications

import (
	"fmt"
	"net/url"

	"github.com/luisc/shepherdr/internal/herdr"
)

const (
	stateVersion     = 1
	MaxSubscriptions = 256
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
	Kind        EventKind
	Status      herdr.Status
	PaneID      string
	TerminalID  string
	Destination string
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
			Kind:        EventStatus,
			Status:      agent.AgentStatus,
			PaneID:      agent.PaneID,
			TerminalID:  agent.TerminalID,
			Destination: destination,
		})
	}

	previousWorkspaces := make(map[string]struct{}, len(previous.Workspaces))
	for _, workspace := range previous.Workspaces {
		previousWorkspaces[workspace.WorkspaceID] = struct{}{}
	}
	currentWorkspaces := make(map[string]struct{}, len(snapshot.Workspaces))
	opened := false
	for _, workspace := range snapshot.Workspaces {
		currentWorkspaces[workspace.WorkspaceID] = struct{}{}
		if _, ok := previousWorkspaces[workspace.WorkspaceID]; !ok {
			opened = true
		}
	}
	closed := false
	for _, workspace := range previous.Workspaces {
		if _, ok := currentWorkspaces[workspace.WorkspaceID]; !ok {
			closed = true
		}
	}
	if opened {
		events = append(events, Event{Kind: EventWorkspaceOpened, Destination: "/"})
	}
	if closed {
		events = append(events, Event{Kind: EventWorkspaceClosed, Destination: "/"})
	}
	return events
}

func terminalKey(paneID, terminalID string) string {
	return fmt.Sprintf("%d:%s%d:%s", len(paneID), paneID, len(terminalID), terminalID)
}
