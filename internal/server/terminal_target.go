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
	for _, candidate := range state.Snapshot.Panes {
		if candidate.PaneID == pane && candidate.TerminalID == terminal {
			found = true
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
					cols: candidate.Rect.Width, generation: state.Gap, pane: pane,
					rows: candidate.Rect.Height, terminal: terminal,
				}, true
			}
		}
	}
	return terminalTarget{}, false
}

func (target terminalTarget) matches(state herdr.State) bool {
	if state.Gap != target.generation {
		return false
	}
	current, ok := terminalTargetFromState(state, target.pane, target.terminal)
	return ok && current.generation == target.generation
}

type terminalTargetLease struct {
	cancel      context.CancelFunc
	done        <-chan struct{}
	source      TerminalStateSource
	target      terminalTarget
	unsubscribe func()
	once        sync.Once
}

func (bridge *TerminalBridge) acquireTarget(pane, terminal string) (*terminalTargetLease, bool) {
	if bridge.targets == nil || !validTerminalIdentity(pane) || !validTerminalIdentity(terminal) {
		return nil, false
	}
	updates, unsubscribe := bridge.targets.Subscribe()
	state, ok := <-updates
	if !ok {
		unsubscribe()
		return nil, false
	}
	target, ok := terminalTargetFromState(state, pane, terminal)
	if !ok {
		unsubscribe()
		return nil, false
	}
	ctx, cancel := context.WithCancel(bridge.context)
	lease := &terminalTargetLease{
		cancel: cancel, done: ctx.Done(), source: bridge.targets, target: target, unsubscribe: unsubscribe,
	}
	go func() {
		for {
			select {
			case state, ok := <-updates:
				if !ok || !target.matches(state) {
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
	return lease.target.matches(lease.source.Current())
}
