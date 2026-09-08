package herdr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/mod/semver"
)

const (
	requestTimeout          = 5 * time.Second
	minimumSupportedVersion = "0.8.0"
	maximumSupportedVersion = "0.9.0"
	protocol20              = 20
	protocol22              = 22
)

type Client struct {
	socketPath           string
	nextID               atomic.Uint64
	logger               *slog.Logger
	compatibilityWarning sync.Once
}

func NewClient(socketPath string, logger *slog.Logger) *Client {
	return &Client{socketPath: socketPath, logger: logger}
}

func (c *Client) SocketPath() string { return c.socketPath }

type responseEnvelope struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type snapshotResult struct {
	Snapshot Snapshot `json:"snapshot"`
	Type     string   `json:"type"`
}

func (c *Client) Snapshot(ctx context.Context) (Snapshot, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	defer conn.Close()

	deadline := time.Now().Add(requestTimeout)
	_ = conn.SetDeadline(deadline)
	id := c.id("snapshot")
	request := map[string]any{
		"id":     id,
		"method": "session.snapshot",
		"params": map[string]any{},
	}
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return Snapshot{}, fmt.Errorf("write Herdr snapshot request: %w", err)
	}

	var envelope responseEnvelope
	if err := json.NewDecoder(conn).Decode(&envelope); err != nil {
		return Snapshot{}, fmt.Errorf("read Herdr snapshot response: %w", err)
	}
	if err := validateResponse(envelope, id); err != nil {
		return Snapshot{}, err
	}
	var result snapshotResult
	if err := json.Unmarshal(envelope.Result, &result); err != nil {
		return Snapshot{}, fmt.Errorf("decode Herdr snapshot: %w", err)
	}
	if result.Type != "session_snapshot" {
		return Snapshot{}, fmt.Errorf("Herdr returned unexpected snapshot response type %q", result.Type)
	}
	warning, err := checkCompatibility(result.Snapshot.Version, result.Snapshot.Protocol)
	if err != nil {
		return Snapshot{}, err
	}
	if warning != "" {
		c.compatibilityWarning.Do(func() {
			c.logger.Warn(warning)
		})
	}
	if _, err := Project(result.Snapshot); err != nil {
		return Snapshot{}, err
	}
	return result.Snapshot, nil
}

func checkCompatibility(version string, protocol uint32) (string, error) {
	parsed, ok := parseSemanticVersion(version)
	if !ok {
		return fmt.Sprintf("Herdr reported version %q, which Shepherdr could not verify; Shepherdr supports stable Herdr %s through %s; continuing best-effort", version, minimumSupportedVersion, maximumSupportedVersion), nil
	}
	minimum := "v" + minimumSupportedVersion
	maximum := "v" + maximumSupportedVersion
	if semver.Compare(parsed, minimum) < 0 {
		return "", &CompatibilityError{Version: version, Protocol: protocol, Reason: CompatibilityVersionTooOld}
	}
	if semver.Compare(parsed, maximum) > 0 {
		return fmt.Sprintf("Herdr %s is newer than Shepherdr's supported stable Herdr range %s through %s; continuing best-effort", version, minimumSupportedVersion, maximumSupportedVersion), nil
	}
	if semver.Prerelease(parsed) != "" {
		return fmt.Sprintf("Herdr %s is outside Shepherdr's supported stable Herdr range %s through %s; continuing best-effort", version, minimumSupportedVersion, maximumSupportedVersion), nil
	}
	stableVersion := strings.TrimPrefix(semver.Canonical(parsed), "v")
	if protocol != Protocol && protocol != protocol20 && protocol != protocol22 {
		return "", &CompatibilityError{Version: version, Protocol: protocol, Reason: CompatibilityInterfaceMismatch}
	}
	if (stableVersion == minimumSupportedVersion && protocol != Protocol) ||
		(stableVersion == "0.8.2" && protocol != protocol20) ||
		(stableVersion == maximumSupportedVersion && protocol != protocol22) ||
		(stableVersion != maximumSupportedVersion && protocol == protocol22) {
		return "", &CompatibilityError{Version: version, Protocol: protocol, Reason: CompatibilityInterfaceMismatch}
	}
	return "", nil
}

func parseSemanticVersion(version string) (string, bool) {
	core := version
	if suffix := strings.IndexAny(core, "+-"); suffix >= 0 {
		core = core[:suffix]
	}
	parsed := "v" + version
	return parsed, strings.Count(core, ".") == 2 && semver.IsValid(parsed)
}

func terminalManagementAvailable(version string) bool {
	parsed, ok := parseSemanticVersion(version)
	if !ok || semver.Prerelease(parsed) != "" {
		return false
	}
	stable := strings.TrimPrefix(semver.Canonical(parsed), "v")
	return stable == minimumSupportedVersion || stable == "0.8.2" || stable == maximumSupportedVersion
}

func TerminalManagementAvailable(version string) bool { return terminalManagementAvailable(version) }

func availableTerminalActions(version string) []TerminalAction {
	if !terminalManagementAvailable(version) {
		return []TerminalAction{}
	}
	return []TerminalAction{TerminalActionSplit, TerminalActionRename}
}

type Subscription struct {
	conn    net.Conn
	decoder *json.Decoder
}

type Event struct {
	Data  json.RawMessage `json:"data"`
	Event string          `json:"event"`
}

func (c *Client) Subscribe(ctx context.Context, snapshot Snapshot) (*Subscription, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}

	subscriptions := []map[string]any{
		{"type": "workspace.created"},
		{"type": "workspace.updated"},
		{"type": "workspace.metadata_updated"},
		{"type": "workspace.renamed"},
		{"type": "workspace.moved"},
		{"type": "workspace.reordered"},
		{"type": "workspace.closed"},
		{"type": "workspace.focused"},
		{"type": "worktree.created"},
		{"type": "worktree.opened"},
		{"type": "worktree.removed"},
		{"type": "tab.created"},
		{"type": "tab.closed"},
		{"type": "tab.renamed"},
		{"type": "tab.moved"},
		{"type": "tab.focused"},
		{"type": "pane.created"},
		{"type": "pane.closed"},
		{"type": "pane.updated"},
		{"type": "pane.moved"},
		{"type": "pane.exited"},
		{"type": "pane.agent_detected"},
		{"type": "pane.focused"},
		{"type": "layout.updated"},
	}
	seen := make(map[string]struct{}, len(snapshot.Panes))
	for _, pane := range snapshot.Panes {
		if _, ok := seen[pane.PaneID]; ok {
			continue
		}
		seen[pane.PaneID] = struct{}{}
		subscriptions = append(subscriptions, map[string]any{
			"type":    "pane.agent_status_changed",
			"pane_id": pane.PaneID,
		})
	}

	id := c.id("subscription")
	request := map[string]any{
		"id":     id,
		"method": "events.subscribe",
		"params": map[string]any{"subscriptions": subscriptions},
	}
	deadline := time.Now().Add(requestTimeout)
	_ = conn.SetDeadline(deadline)
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		conn.Close()
		return nil, fmt.Errorf("write Herdr subscription request: %w", err)
	}
	decoder := json.NewDecoder(conn)
	var envelope responseEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		conn.Close()
		return nil, fmt.Errorf("read Herdr subscription response: %w", err)
	}
	if err := validateResponse(envelope, id); err != nil {
		conn.Close()
		return nil, err
	}
	var result struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(envelope.Result, &result); err != nil {
		conn.Close()
		return nil, fmt.Errorf("decode Herdr subscription response: %w", err)
	}
	if result.Type != "subscription_started" {
		conn.Close()
		return nil, fmt.Errorf("Herdr returned unexpected subscription response type %q", result.Type)
	}
	_ = conn.SetDeadline(time.Time{})
	return &Subscription{conn: conn, decoder: decoder}, nil
}

func (s *Subscription) Next() (Event, error) {
	var event Event
	if err := s.decoder.Decode(&event); err != nil {
		return Event{}, err
	}
	if event.Event == "" {
		return Event{}, errors.New("Herdr subscription returned a non-event message")
	}
	return event, nil
}

func (s *Subscription) Close() error { return s.conn.Close() }

func (c *Client) dial(ctx context.Context) (net.Conn, error) {
	dialer := net.Dialer{Timeout: requestTimeout}
	conn, err := dialer.DialContext(ctx, "unix", c.socketPath)
	if err != nil {
		return nil, fmt.Errorf("connect to Herdr: %w", err)
	}
	return conn, nil
}

func (c *Client) id(prefix string) string {
	return fmt.Sprintf("shepherdr:%s:%d", prefix, c.nextID.Add(1))
}

func validateResponse(envelope responseEnvelope, id string) error {
	if envelope.ID != id {
		return fmt.Errorf("Herdr response id %q did not match request %q", envelope.ID, id)
	}
	if envelope.Error != nil {
		return &APIError{Code: envelope.Error.Code, Message: envelope.Error.Message}
	}
	if len(envelope.Result) == 0 {
		return errors.New("Herdr response omitted its result")
	}
	return nil
}

func IsNotRunning(err error) bool {
	if err == nil {
		return false
	}
	var netErr *net.OpError
	if errors.As(err, &netErr) {
		return true
	}
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, net.ErrClosed) || errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE)
}
