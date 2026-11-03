package herdr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Herdr clients share which agents a person has seen and which are pinned
// through two terminal labels (the Herdr app's docs/labels.md).
const (
	// CoreSeenLabel holds the last turn a person looked at.
	CoreSeenLabel = "herdr.seen"
	// CorePinnedLabel is present and non-empty while the agent is pinned.
	CorePinnedLabel = "herdr.pinned"
)

// coreSeenTurn reads herdr.seen as the Herdr app does: an unsigned decimal
// number, optionally after one "+". Anything else counts as missing.
func coreSeenTurn(labels map[string]string) (uint64, bool) {
	value, present := labels[CoreSeenLabel]
	if !present {
		return 0, false
	}
	turn, err := strconv.ParseUint(strings.TrimPrefix(value, "+"), 10, 64)
	return turn, err == nil
}

// CoreShownStatus is the status Home shows, by the Herdr app's rule: an
// agent that reports done or idle is done while its last turn is unseen
// (turn > herdr.seen) and idle once someone has looked. Working, blocked and
// error, and any agent without a usable herdr.seen, keep core's own word.
// Notifications keep core's word.
func CoreShownStatus(terminal CoreTerminal) Status {
	if terminal.Status == nil {
		return ""
	}
	status := Status(terminal.Status.State)
	seen, ok := coreSeenTurn(terminal.Labels)
	if !ok || status != StatusDone && status != StatusIdle {
		return status
	}
	if terminal.Turn > seen {
		return StatusDone
	}
	return StatusIdle
}

// CoreSeenNeedsRaise says whether a person looking at the terminal now should
// write its current turn: herdr.seen is only ever raised, and is written when
// it is missing.
func CoreSeenNeedsRaise(terminal CoreTerminal) bool {
	seen, ok := coreSeenTurn(terminal.Labels)
	return !ok || seen < terminal.Turn
}

func corePinned(terminal CoreTerminal) bool {
	return terminal.Labels[CorePinnedLabel] != ""
}

// CoreTerminalFollow follows one terminal's facts on its own event
// connection: the reply's snapshot, then each status, turn and labels change.
type CoreTerminalFollow struct {
	subscription *CoreSubscription
	terminal     CoreTerminal
}

// ErrCoreTerminalGone means the followed terminal finished or was removed.
var ErrCoreTerminalGone = errors.New("Herdr Core terminal is no longer live")

// FollowTerminal subscribes to one terminal of this machine by its exact
// address.
func (c *CoreClient) FollowTerminal(ctx context.Context, address string) (*CoreTerminalFollow, error) {
	if address == "" || strings.Contains(address, "/") {
		return nil, errors.New("follow target is invalid")
	}
	connection, _, err := c.open(ctx)
	if err != nil {
		return nil, err
	}
	var started struct {
		Seq       *uint64        `json:"seq"`
		Terminals []CoreTerminal `json:"terminals"`
	}
	if err := connection.call("events.subscribe", map[string]any{"terminal": address}, &started); err != nil {
		connection.conn.Close()
		return nil, err
	}
	if started.Seq == nil || len(started.Terminals) != 1 || started.Terminals[0].Address != address {
		connection.conn.Close()
		return nil, fmt.Errorf("Herdr Core did not follow terminal %q", address)
	}
	return &CoreTerminalFollow{
		subscription: &CoreSubscription{conn: connection.conn, decoder: connection.decoder},
		terminal:     started.Terminals[0],
	}, nil
}

// Terminal is the followed terminal as last seen.
func (f *CoreTerminalFollow) Terminal() CoreTerminal { return f.terminal }

// Next waits for the next change and reports whether it was a turn ending,
// which is when a client that is looking decides to mark it seen.
func (f *CoreTerminalFollow) Next() (turnEnded bool, err error) {
	_ = f.subscription.conn.SetReadDeadline(time.Now().Add(coreEventSilence))
	var message struct {
		Method string `json:"method"`
		Params struct {
			Terminal string          `json:"terminal"`
			Type     string          `json:"type"`
			Data     json.RawMessage `json:"data"`
		} `json:"params"`
	}
	if err := f.subscription.decoder.Decode(&message); err != nil {
		return false, err
	}
	if message.Method == "missed" {
		return false, errors.New("Herdr Core terminal events were missed")
	}
	if message.Method != "event" || message.Params.Terminal != f.terminal.Address {
		return false, nil
	}
	switch message.Params.Type {
	case "status":
		var data struct {
			Status *CoreStatus `json:"status"`
		}
		if err := json.Unmarshal(message.Params.Data, &data); err != nil {
			return false, err
		}
		f.terminal.Status = data.Status
	case "turn":
		var data struct {
			Turn *uint64 `json:"turn"`
		}
		if err := json.Unmarshal(message.Params.Data, &data); err != nil || data.Turn == nil {
			return false, errors.New("Herdr Core sent an unreadable turn")
		}
		f.terminal.Turn = *data.Turn
		return true, nil
	case "labels":
		var data struct {
			Labels map[string]string `json:"labels"`
		}
		if err := json.Unmarshal(message.Params.Data, &data); err != nil || data.Labels == nil {
			return false, errors.New("Herdr Core sent unreadable labels")
		}
		f.terminal.Labels = data.Labels
	case "finished", "removed":
		return false, ErrCoreTerminalGone
	}
	return false, nil
}

func (f *CoreTerminalFollow) Close() error { return f.subscription.Close() }
