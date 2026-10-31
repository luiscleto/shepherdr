package server

import (
	"context"
	"sync"

	"github.com/luiscleto/shepherdr/internal/herdr"
)

type TerminalStateSource interface {
	Current() herdr.State
	Subscribe() (<-chan herdr.State, func())
}

func validTerminalIdentity(value string) bool {
	if value == "" || len(value) > 256 {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		alphanumeric := character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9'
		if alphanumeric || index > 0 && (character == '_' || character == ':' || character == '.' || character == '-') {
			continue
		}
		return false
	}
	return true
}

type terminalTarget struct {
	workspace  string
	tab        string
	cols       int
	generation uint64
	pane       string
	rows       int
	terminal   string
}

func terminalTargetFromState(state herdr.State, pane, terminal string) (terminalTarget, bool) {
	if state.Connection != herdr.ConnectionLive || state.LastKnown {
		return terminalTarget{}, false
	}
	found := false
	var bound herdr.PaneInfo
	for _, candidate := range state.Snapshot.Panes {
		if candidate.PaneID == pane && candidate.TerminalID == terminal {
			found = true
			bound = candidate
			break
		}
	}
	if !found {
		return terminalTarget{}, false
	}
	for _, layout := range state.Snapshot.Layouts {
		for _, candidate := range layout.Panes {
			if candidate.PaneID == pane && candidate.Rect.Width >= 2 && candidate.Rect.Height >= 1 {
				return terminalTarget{
					workspace: bound.WorkspaceID, tab: bound.TabID,
					cols: candidate.Rect.Width, generation: state.Gap, pane: pane,
					rows: candidate.Rect.Height, terminal: terminal,
				}, true
			}
		}
	}
	return terminalTarget{}, false
}

// coreTerminalTargetFromState is the Herdr Core target rule: Home is live and
// current, and still holds this address with the same terminal identity.
// Core has no layout rectangles to check.
func coreTerminalTargetFromState(state herdr.State, pane, terminal string) (terminalTarget, bool) {
	if state.Connection != herdr.ConnectionLive || state.LastKnown {
		return terminalTarget{}, false
	}
	for _, candidate := range state.Snapshot.Panes {
		if candidate.PaneID == pane && candidate.TerminalID == terminal {
			return terminalTarget{
				workspace: candidate.WorkspaceID, tab: candidate.TabID, generation: state.Gap, pane: pane, terminal: terminal,
			}, true
		}
	}
	return terminalTarget{}, false
}

type terminalTargetRule func(state herdr.State, pane, terminal string) (terminalTarget, bool)

func (target terminalTarget) matches(state herdr.State, rule terminalTargetRule) bool {
	if state.Gap != target.generation {
		return false
	}
	current, ok := rule(state, target.pane, target.terminal)
	return ok && current.generation == target.generation && current.workspace == target.workspace && current.tab == target.tab
}

type terminalTargetLease struct {
	cancel      context.CancelFunc
	done        <-chan struct{}
	rule        terminalTargetRule
	source      TerminalStateSource
	target      terminalTarget
	unsubscribe func()
	once        sync.Once
}

func (bridge *TerminalBridge) acquireTarget(pane, terminal string) (*terminalTargetLease, bool) {
	return acquireTerminalTarget(bridge.context, bridge.targets, terminalTargetFromState, pane, terminal)
}

// acquireTerminalTarget holds a target while the source's state keeps
// matching it under rule. The lease ends at the first state that doesn't.
func acquireTerminalTarget(parent context.Context, source TerminalStateSource, rule terminalTargetRule, pane, terminal string) (*terminalTargetLease, bool) {
	if source == nil || !validTerminalIdentity(pane) || !validTerminalIdentity(terminal) {
		return nil, false
	}
	updates, unsubscribe := source.Subscribe()
	state, ok := <-updates
	if !ok {
		unsubscribe()
		return nil, false
	}
	target, ok := rule(state, pane, terminal)
	if !ok {
		unsubscribe()
		return nil, false
	}
	ctx, cancel := context.WithCancel(parent)
	lease := &terminalTargetLease{
		cancel: cancel, done: ctx.Done(), rule: rule, source: source, target: target, unsubscribe: unsubscribe,
	}
	go func() {
		for {
			select {
			case state, ok := <-updates:
				if !ok || !target.matches(state, rule) {
					cancel()
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return lease, true
}

func (lease *terminalTargetLease) Close() {
	lease.once.Do(func() {
		lease.cancel()
		lease.unsubscribe()
	})
}

func (lease *terminalTargetLease) Valid() bool {
	select {
	case <-lease.done:
		return false
	default:
	}
	return lease.target.matches(lease.source.Current(), lease.rule)
}
