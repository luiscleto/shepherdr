package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/luiscleto/shepherdr/internal/herdr"
)

// This is a logical key selection, never terminal bytes or a destination.
type terminalKey struct {
	Base  string `json:"base"`
	Ctrl  bool   `json:"ctrl"`
	Alt   bool   `json:"alt"`
	Shift bool   `json:"shift"`
	Super bool   `json:"super"`
	Hyper bool   `json:"hyper"`
}

func (key terminalKey) canonical() (string, error) {
	base := strings.ToLower(key.Base)
	switch base {
	case "enter", "return", "esc", "escape", "tab", "backspace", "bs", "left", "up", "down", "right",
		"space", "minus", "comma", "period", "slash", "backslash", "quote", "double_quote", "double-quote",
		"semicolon", "colon", "percent", "ampersand", "backtick", "plus":
	default:
		if key.Base == " " {
			base = "space"
		} else if key.Base == "+" {
			base = "plus"
		} else if utf8.ValidString(key.Base) && utf8.RuneCountInString(key.Base) == 1 {
			r, _ := utf8.DecodeRuneInString(key.Base)
			if !unicode.IsPrint(r) || unicode.IsSpace(r) {
				return "", errors.New("invalid key character")
			}
			base = key.Base // Herdr applies implicit Shift to uppercase ASCII.
		} else {
			n, err := strconv.Atoi(strings.TrimPrefix(base, "f"))
			if err != nil || n < 1 || n > 12 || base != "f"+strconv.Itoa(n) {
				return "", errors.New("unsupported key")
			}
		}
	}
	parts := []string{}
	for _, modifier := range []struct {
		name    string
		enabled bool
	}{
		{"ctrl", key.Ctrl}, {"alt", key.Alt}, {"shift", key.Shift}, {"super", key.Super}, {"hyper", key.Hyper},
	} {
		if modifier.enabled {
			parts = append(parts, modifier.name)
		}
	}
	return strings.Join(append(parts, base), "+"), nil
}

func validatedTerminalKeyCommand(message []byte) (terminalBrowserCommand, error) {
	var envelope struct {
		Type      string       `json:"type"`
		RequestID uint64       `json:"request_id"`
		Key       *terminalKey `json:"key"`
	}
	decoder := json.NewDecoder(bytes.NewReader(message))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&envelope)
	command := terminalBrowserCommand{isKey: true, requestID: envelope.RequestID}
	if err != nil || !utf8.Valid(message) || requireJSONEnd(decoder) != nil || envelope.Key == nil || envelope.RequestID < 1 || envelope.RequestID > 1<<53-1 {
		return command, errors.New("invalid key command")
	}
	var fields struct {
		Key map[string]json.RawMessage `json:"key"`
	}
	_ = json.Unmarshal(message, &fields)
	// encoding/json replaces an unpaired escaped surrogate with U+FFFD. Only
	// accept that scalar when the request actually spells the replacement char.
	if envelope.Key.Base == "\ufffd" {
		raw := strings.TrimSpace(string(fields.Key["base"]))
		if raw != `"�"` && !strings.EqualFold(raw, `"\ufffd"`) {
			return command, errors.New("invalid Unicode key character")
		}
	}
	for _, value := range fields.Key {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return command, errors.New("invalid key field")
		}
	}
	command.key, err = envelope.Key.canonical()
	return command, err
}

// The caller holds the same write lock as text, files and Release. Herdr owns
// encoding and queuing; this RPC cannot atomically bind its final pane lookup
// to our controller, or guarantee arrival order with earlier stream writes.
func (bridge *TerminalBridge) sendTerminalKey(request *http.Request, session *terminalChildSession, lease *terminalTargetLease, key string) (terminalBatchOutcome, error) {
	check := func() error {
		if request.Context().Err() != nil {
			return request.Context().Err()
		}
		if bridge.context.Err() != nil {
			return bridge.context.Err()
		}
		if lease == nil || !lease.Valid() || lease.target.workspace == "" || lease.target.tab == "" {
			return errors.New("terminal unavailable")
		}
		if !session.controlled || session.inputEnded.Load() || session.settings.mode == "observe" {
			return errors.New("terminal is not controlled")
		}
		select {
		case <-session.exited:
			return errors.New("controlling child ended")
		default:
		}
		return nil
	}
	if err := check(); err != nil {
		return terminalBatchNotSent, err
	}
	ctx, cancel := bridge.commandContext(request.Context(), lease.done)
	defer cancel()
	snapshot, err := bridge.keys.Snapshot(ctx)
	if err != nil {
		return terminalBatchNotSent, err
	}
	target, ok := terminalTargetFromState(herdr.State{Connection: herdr.ConnectionLive, Snapshot: snapshot}, lease.target.pane, lease.target.terminal)
	if !ok || target.workspace != lease.target.workspace || target.tab != lease.target.tab {
		return terminalBatchNotSent, errors.New("terminal was replaced")
	}
	submitted := false
	err = withCommitAuthority(request, func() error {
		if err := check(); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		submitted = true
		return bridge.keys.SendKeys(ctx, lease.target.pane, key)
	})
	if err != nil {
		if herdr.MutationMayHaveRun(err) {
			return terminalBatchUnknown, err
		}
		return terminalBatchNotSent, err
	}
	if submitted && (ctx.Err() != nil || !requestAuthorityValid(request)) {
		return terminalBatchUnknown, context.Canceled
	}
	return terminalBatchForwarded, nil
}
