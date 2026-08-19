package herdr

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"time"
)

type Connection string

const projectorRetryDelay = time.Second

const (
	ConnectionReconnecting Connection = "reconnecting"
	ConnectionLive         Connection = "live"
	ConnectionNotRunning   Connection = "not_running"
	ConnectionIncompatible Connection = "incompatible"
)

type State struct {
	Connection Connection `json:"connection"`
	Detail     string     `json:"detail,omitempty"`
	Gap        uint64     `json:"gap"`
	Home       Home       `json:"home"`
	LastKnown  bool       `json:"last_known"`
	Snapshot   Snapshot   `json:"-"`
}

type Projector struct {
	client *Client

	mu        sync.RWMutex
	state     State
	listeners map[chan State]struct{}
}

func NewProjector(client *Client) *Projector {
	return &Projector{
		client: client,
		state: State{
			Connection: ConnectionReconnecting,
			Home:       Home{Workspaces: []Workspace{}},
		},
		listeners: make(map[chan State]struct{}),
	}
}

func (p *Projector) Current() State {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.state
}

func (p *Projector) Subscribe() (<-chan State, func()) {
	updates := make(chan State, 1)
	p.mu.Lock()
	p.listeners[updates] = struct{}{}
	updates <- p.state
	p.mu.Unlock()
	return updates, func() {
		p.mu.Lock()
		delete(p.listeners, updates)
		close(updates)
		p.mu.Unlock()
	}
}

func (p *Projector) Run(ctx context.Context) {
	retry := time.NewTimer(0)
	defer retry.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-retry.C:
		}

		p.markReconnecting()
		first, err := p.client.Snapshot(ctx)
		if err != nil {
			p.publishError(err)
			resetTimer(retry, projectorRetryDelay)
			continue
		}
		subscription, err := p.client.Subscribe(ctx, first)
		if err != nil {
			p.publishError(err)
			resetTimer(retry, projectorRetryDelay)
			continue
		}
		current, err := p.client.Snapshot(ctx)
		if err != nil {
			subscription.Close()
			p.publishError(err)
			resetTimer(retry, projectorRetryDelay)
			continue
		}
		if !subscriptionCoversSnapshot(first, current) {
			subscription.Close()
			resetTimer(retry, projectorRetryDelay)
			continue
		}
		p.publishLive(current)

		for {
			event, err := subscription.Next()
			if err != nil {
				subscription.Close()
				p.markGap()
				resetTimer(retry, projectorRetryDelay)
				break
			}
			if !semanticEvent(event.Event) {
				subscription.Close()
				p.markGap()
				resetTimer(retry, projectorRetryDelay)
				break
			}
			current, err = p.client.Snapshot(ctx)
			if err != nil {
				subscription.Close()
				p.publishError(err)
				resetTimer(retry, projectorRetryDelay)
				break
			}
			if !subscriptionCoversSnapshot(first, current) {
				subscription.Close()
				p.markReconnecting()
				resetTimer(retry, projectorRetryDelay)
				break
			}
			p.publishLive(current)
		}
	}
}

func subscriptionCoversSnapshot(subscribed, candidate Snapshot) bool {
	covered := make(map[string]struct{}, len(subscribed.Panes))
	for _, pane := range subscribed.Panes {
		covered[pane.PaneID] = struct{}{}
	}
	for _, pane := range candidate.Panes {
		if _, ok := covered[pane.PaneID]; !ok {
			return false
		}
	}
	return true
}

func semanticEvent(event string) bool {
	// Herdr subscription selectors use dotted names, but streamed event envelopes
	// use the schema's underscored EventKind values.
	switch event {
	case "workspace_created", "workspace_updated", "workspace_metadata_updated", "workspace_renamed", "workspace_moved", "workspace_reordered", "workspace_closed", "workspace_focused",
		"tab_created", "tab_closed", "tab_renamed", "tab_moved", "tab_focused",
		"pane_created", "pane_closed", "pane_updated", "pane_moved", "pane_exited", "pane_agent_detected", "pane_focused", "pane_agent_status_changed",
		"layout_updated":
		return true
	default:
		return false
	}
}

func (p *Projector) markReconnecting() {
	p.mu.Lock()
	state := p.state
	hadKnownState := state.Connection == ConnectionLive || state.LastKnown
	state.Connection = ConnectionReconnecting
	state.Detail = ""
	state.LastKnown = hadKnownState
	p.publishLocked(state)
	p.mu.Unlock()
}

func (p *Projector) markGap() {
	p.mu.Lock()
	state := p.state
	state.Connection = ConnectionReconnecting
	state.Detail = ""
	state.Gap++
	state.LastKnown = true
	p.publishLocked(state)
	p.mu.Unlock()
}

func (p *Projector) publishError(err error) {
	p.mu.Lock()
	state := p.state
	if state.Connection == ConnectionLive {
		state.Gap++
	}
	state.LastKnown = state.Connection == ConnectionLive || state.LastKnown
	var protocol *ProtocolError
	switch {
	case IsNotRunning(err):
		state.Connection = ConnectionNotRunning
		state.Detail = ""
	case errors.As(err, &protocol):
		state.Connection = ConnectionIncompatible
		state.Detail = protocol.Error()
	default:
		state.Connection = ConnectionIncompatible
		state.Detail = err.Error()
	}
	p.publishLocked(state)
	p.mu.Unlock()
}

func (p *Projector) publishLive(snapshot Snapshot) {
	home, err := Project(snapshot)
	if err != nil {
		p.publishError(err)
		return
	}
	p.mu.Lock()
	state := p.state
	state.Connection = ConnectionLive
	state.Detail = ""
	state.Home = home
	state.LastKnown = false
	state.Snapshot = snapshot
	p.publishLocked(state)
	p.mu.Unlock()
}

func (p *Projector) publishLocked(state State) {
	previous := p.state
	p.state = state
	if samePublishedState(previous, state) {
		return
	}
	for listener := range p.listeners {
		select {
		case listener <- state:
		default:
			select {
			case <-listener:
			default:
			}
			listener <- state
		}
	}
}

func samePublishedState(left, right State) bool {
	return left.Connection == right.Connection &&
		left.Detail == right.Detail &&
		left.Gap == right.Gap &&
		left.LastKnown == right.LastKnown &&
		reflect.DeepEqual(left.Home, right.Home)
}

func resetTimer(timer *time.Timer, duration time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(duration)
}
