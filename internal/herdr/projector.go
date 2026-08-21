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
	HasHome    bool       `json:"has_home"`
	Home       Home       `json:"home"`
	LastKnown  bool       `json:"last_known"`
	Snapshot   Snapshot   `json:"-"`
}

type SnapshotObserver interface {
	ObserveSnapshot(Snapshot, bool)
}

type Projector struct {
	client  *Client
	refresh chan struct{}

	mu        sync.RWMutex
	state     State
	listeners map[chan State]struct{}
	observer  SnapshotObserver
}

func (p *Projector) SetSnapshotObserver(observer SnapshotObserver) {
	p.mu.Lock()
	p.observer = observer
	p.mu.Unlock()
}

func NewProjector(client *Client) *Projector {
	return &Projector{
		client:  client,
		refresh: make(chan struct{}, 1),
		state: State{
			Connection: ConnectionReconnecting,
			Home:       Home{Workspaces: []Workspace{}},
		},
		listeners: make(map[chan State]struct{}),
	}
}

func (p *Projector) RequestRefresh() {
	select {
	case p.refresh <- struct{}{}:
	default:
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
	var subscriptionBasis Snapshot
	haveSubscriptionBasis := false
	baseline := true

	for {
		if !haveSubscriptionBasis {
			select {
			case <-ctx.Done():
				return
			case <-retry.C:
			}

			setup, err := p.client.Snapshot(ctx)
			if err != nil {
				p.publishError(err)
				resetTimer(retry, projectorRetryDelay)
				continue
			}
			subscriptionBasis = setup
		}

		subscription, err := p.client.Subscribe(ctx, subscriptionBasis)
		if err != nil {
			p.publishError(err)
			haveSubscriptionBasis = false
			resetTimer(retry, projectorRetryDelay)
			continue
		}

		result := p.followSubscription(ctx, subscription, subscriptionBasis, baseline)
		_ = subscription.Close()
		if result.resubscribe {
			subscriptionBasis = result.snapshot
			haveSubscriptionBasis = true
			baseline = result.baseline
			continue
		}
		haveSubscriptionBasis = false
		baseline = true
		if result.err != nil && ctx.Err() == nil {
			if result.snapshotFailed {
				p.publishError(result.err)
			} else {
				p.markGap()
			}
			resetTimer(retry, projectorRetryDelay)
		}
	}
}

type subscriptionResult struct {
	err            error
	resubscribe    bool
	snapshot       Snapshot
	snapshotFailed bool
	baseline       bool
}

func (p *Projector) followSubscription(ctx context.Context, subscription *Subscription, basis Snapshot, baseline bool) subscriptionResult {
	changed := make(chan struct{}, 1)
	lost := make(chan error, 1)
	go func() {
		for {
			event, err := subscription.Next()
			if err == nil && !semanticEvent(event.Event) {
				err = errors.New("Herdr subscription returned an unexpected event")
			}
			if err != nil {
				lost <- err
				return
			}
			select {
			case changed <- struct{}{}:
			default:
			}
		}
	}()

	readNow := true
	for {
		if !readNow {
			select {
			case <-ctx.Done():
				return subscriptionResult{err: ctx.Err()}
			case err := <-lost:
				return subscriptionResult{err: err}
			case <-changed:
			case <-p.refresh:
			}
		}
		takeRefreshSignals(changed, p.refresh)

		candidate, err := p.client.Snapshot(ctx)
		if err != nil {
			return subscriptionResult{err: err, snapshotFailed: true}
		}
		select {
		case err := <-lost:
			return subscriptionResult{err: err}
		default:
		}
		if !subscriptionCoversSnapshot(basis, candidate) {
			return subscriptionResult{resubscribe: true, snapshot: candidate, baseline: baseline}
		}
		if p.publishLiveObserved(candidate, baseline) {
			baseline = false
		} else {
			baseline = true
		}
		readNow = takeRefreshSignals(changed, p.refresh)
	}
}

func takeRefreshSignals(changed, requested <-chan struct{}) bool {
	dirty := false
	select {
	case <-changed:
		dirty = true
	default:
	}
	select {
	case <-requested:
		dirty = true
	default:
	}
	return dirty
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
		"worktree_created", "worktree_opened", "worktree_removed",
		"tab_created", "tab_closed", "tab_renamed", "tab_moved", "tab_focused",
		"pane_created", "pane_closed", "pane_updated", "pane_moved", "pane_exited", "pane_agent_detected", "pane_focused", "pane_agent_status_changed",
		"layout_updated":
		return true
	default:
		return false
	}
}

func (p *Projector) markGap() {
	p.mu.Lock()
	state := p.state
	state.Connection = ConnectionReconnecting
	state.Detail = ""
	state.Gap++
	state.LastKnown = state.HasHome
	p.publishLocked(state)
	p.mu.Unlock()
}

func (p *Projector) publishError(err error) {
	p.mu.Lock()
	state := p.state
	if state.Connection == ConnectionLive {
		state.Gap++
	}
	state.LastKnown = state.HasHome
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
	p.publishLiveObserved(snapshot, false)
}

func (p *Projector) publishLiveObserved(snapshot Snapshot, baseline bool) bool {
	home, err := Project(snapshot)
	if err != nil {
		p.publishError(err)
		return false
	}
	p.mu.RLock()
	observer := p.observer
	p.mu.RUnlock()
	if observer != nil {
		observer.ObserveSnapshot(snapshot, baseline)
	}
	p.mu.Lock()
	state := p.state
	state.Connection = ConnectionLive
	state.Detail = ""
	state.HasHome = true
	state.Home = home
	state.LastKnown = false
	state.Snapshot = snapshot
	p.publishLocked(state)
	p.mu.Unlock()
	return true
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
		left.HasHome == right.HasHome &&
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
