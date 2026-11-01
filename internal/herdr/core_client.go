package herdr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/mod/semver"
)

const (
	coreGeneration              = 1
	minimumSupportedCoreVersion = "0.1.15"
	maximumSupportedCoreVersion = "0.1.16"
	// Core sends a heartbeat every 15 s on an event connection. Three missed
	// heartbeats mean the connection is gone even if the socket stays open.
	coreEventSilence = 45 * time.Second
	coreDocumentName = "herdr-app"
)

var (
	coreRequiredFeatures = []string{"events", "framed", "attach"}
	coreRequiredMethods  = []string{"hello", "events.subscribe", "terminal.list", "doc.get", "terminal.attach", "terminal.keys", "terminal.kill", "terminal.run"}
)

// CoreClient reads Home from herdr-core, the runtime behind the Herdr app.
// It only dials the socket: it never runs hc, which could start a core.
type CoreClient struct {
	socketPath           string
	nextID               atomic.Uint64
	logger               *slog.Logger
	compatibilityWarning sync.Once
}

func NewCoreClient(socketPath string, logger *slog.Logger) *CoreClient {
	return &CoreClient{socketPath: socketPath, logger: logger}
}

func (c *CoreClient) SocketPath() string { return c.socketPath }

// CoreRead is one complete read: the core's identity, its live terminals and
// the Herdr app's layout document.
type CoreRead struct {
	CoreID    string
	Version   string
	Terminals []CoreTerminal
	Document  json.RawMessage
	// AppWindow is the Herdr app window's state just before this read, which
	// picks the actions Home offers. Read leaves it AppWindowUnpaired.
	AppWindow AppWindowState
	// AppWindowOpened is when Shepherdr first saw the window open, while
	// AppWindow is AppWindowOpen. Zero leaves it unknown.
	AppWindowOpened time.Time
}

// AppWindowState is what Shepherdr knows about the Herdr app window.
type AppWindowState int

const (
	// AppWindowUnpaired: there is no app client, or the window hasn't been
	// asked yet. Home offers no app actions.
	AppWindowUnpaired AppWindowState = iota
	AppWindowOpen
	// AppWindowClosed: New space and New terminal start in core, and the app
	// adds them when its window next opens.
	AppWindowClosed
)

type CoreTerminal struct {
	Address       string            `json:"address"`
	CreatedMS     uint64            `json:"created_ms"`
	CWD           string            `json:"cwd"`
	DieWithParent bool              `json:"die_with_parent"`
	Foreground    *CoreForeground   `json:"foreground"`
	Labels        map[string]string `json:"labels"`
	Name          *string           `json:"name"`
	Parent        *string           `json:"parent"`
	Status        *CoreStatus       `json:"status"`
	Title         string            `json:"title"`
}

// CoreForeground is the program in front. Its cwd is the terminal's current
// folder; CoreTerminal.CWD is only the folder it started in.
type CoreForeground struct {
	CWD string `json:"cwd"`
}

// Folder is the terminal's current folder, else the one it started in.
func (t CoreTerminal) Folder() string {
	if t.Foreground != nil && t.Foreground.CWD != "" {
		return t.Foreground.CWD
	}
	return t.CWD
}

type CoreStatus struct {
	App   string `json:"app"`
	State string `json:"state"`
}

type coreHello struct {
	Core struct {
		ID string `json:"id"`
	} `json:"core"`
	Features   []string `json:"features"`
	Generation uint32   `json:"generation"`
	Methods    []string `json:"methods"`
	Version    string   `json:"version"`
}

type coreResponse struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
		Data    struct {
			Kind string `json:"kind"`
		} `json:"data"`
	} `json:"error"`
}

type coreConnection struct {
	client  *CoreClient
	conn    net.Conn
	decoder *json.Decoder
}

func (c *CoreClient) open(ctx context.Context) (*coreConnection, coreHello, error) {
	dialer := net.Dialer{Timeout: requestTimeout}
	conn, err := dialer.DialContext(ctx, "unix", c.socketPath)
	if err != nil {
		return nil, coreHello{}, fmt.Errorf("connect to Herdr Core: %w", err)
	}
	_ = conn.SetDeadline(time.Now().Add(requestTimeout))
	connection := &coreConnection{client: c, conn: conn, decoder: json.NewDecoder(conn)}
	var hello coreHello
	if err := connection.call("hello", map[string]any{"generation": coreGeneration}, &hello); err != nil {
		conn.Close()
		var apiError *APIError
		if errors.As(err, &apiError) && apiError.Code == "unsupported_generation" {
			return nil, coreHello{}, coreCannotServe("protocol generation 1")
		}
		return nil, coreHello{}, err
	}
	if err := c.checkHello(hello); err != nil {
		conn.Close()
		return nil, coreHello{}, err
	}
	return connection, hello, nil
}

func (c *CoreClient) checkHello(hello coreHello) error {
	if hello.Generation != coreGeneration {
		return coreCannotServe("protocol generation 1")
	}
	if hello.Core.ID == "" {
		return errors.New("Herdr Core did not report its identity")
	}
	for _, feature := range coreRequiredFeatures {
		if !contains(hello.Features, feature) {
			return coreCannotServe(feature)
		}
	}
	for _, method := range coreRequiredMethods {
		if !contains(hello.Methods, method) {
			return coreCannotServe(method)
		}
	}
	warning, err := checkCoreVersion(hello.Version)
	if err != nil {
		return err
	}
	if warning != "" {
		c.compatibilityWarning.Do(func() { c.logger.Warn(warning) })
	}
	return nil
}

func checkCoreVersion(version string) (string, error) {
	parsed, ok := parseSemanticVersion(version)
	if !ok {
		return fmt.Sprintf("Herdr Core reported version %q, which Shepherdr could not verify; Shepherdr supports Herdr Core %s through %s; continuing best-effort", version, minimumSupportedCoreVersion, maximumSupportedCoreVersion), nil
	}
	if semver.Compare(parsed, "v"+minimumSupportedCoreVersion) < 0 {
		return "", &CoreCompatibilityError{Detail: fmt.Sprintf("Herdr Core %s is older than the supported range; Shepherdr supports Herdr Core %s through %s.", version, minimumSupportedCoreVersion, maximumSupportedCoreVersion)}
	}
	if semver.Compare(parsed, "v"+maximumSupportedCoreVersion) > 0 || semver.Prerelease(parsed) != "" {
		return fmt.Sprintf("Herdr Core %s is outside Shepherdr's supported Herdr Core range %s through %s; continuing best-effort", version, minimumSupportedCoreVersion, maximumSupportedCoreVersion), nil
	}
	return "", nil
}

func coreCannotServe(missing string) error {
	return &CoreCompatibilityError{Detail: fmt.Sprintf("This Herdr Core can't serve Shepherdr: it doesn't offer %s.", missing)}
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func (connection *coreConnection) call(method string, params any, result any) error {
	id := connection.client.nextID.Add(1)
	request := map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}
	if err := json.NewEncoder(connection.conn).Encode(request); err != nil {
		return fmt.Errorf("write Herdr Core %s request: %w", method, err)
	}
	var response coreResponse
	if err := connection.decoder.Decode(&response); err != nil {
		return fmt.Errorf("read Herdr Core %s response: %w", method, err)
	}
	if string(response.ID) != fmt.Sprint(id) {
		return fmt.Errorf("Herdr Core response id %s did not match request %d", response.ID, id)
	}
	if response.Error != nil {
		return &APIError{Code: response.Error.Data.Kind, Message: response.Error.Message}
	}
	if len(response.Result) == 0 {
		return fmt.Errorf("Herdr Core %s response omitted its result", method)
	}
	if err := json.Unmarshal(response.Result, result); err != nil {
		return fmt.Errorf("decode Herdr Core %s response: %w", method, err)
	}
	return nil
}

// Read does one complete read on its own short-lived connection.
func (c *CoreClient) Read(ctx context.Context) (CoreRead, error) {
	connection, hello, err := c.open(ctx)
	if err != nil {
		return CoreRead{}, err
	}
	defer connection.conn.Close()
	var list struct {
		Terminals []CoreTerminal `json:"terminals"`
	}
	if err := connection.call("terminal.list", map[string]any{}, &list); err != nil {
		return CoreRead{}, err
	}
	var document struct {
		Value json.RawMessage `json:"value"`
	}
	if err := connection.call("doc.get", map[string]any{"name": coreDocumentName}, &document); err != nil {
		return CoreRead{}, err
	}
	return CoreRead{CoreID: hello.Core.ID, Version: hello.Version, Terminals: list.Terminals, Document: document.Value}, nil
}

// Kill ends one terminal of this machine by its exact address, never a name,
// and returns the state core observed. Core answers a terminal that had
// already finished with its finished record, so whether it was live must come
// from a fresh read before the kill.
func (c *CoreClient) Kill(ctx context.Context, address string) (string, error) {
	if address == "" || strings.Contains(address, "/") {
		return "", &MutationError{Err: errors.New("kill target is invalid")}
	}
	connection, _, err := c.open(ctx)
	if err != nil {
		return "", &MutationError{Err: err}
	}
	defer connection.conn.Close()
	var result struct {
		Address string `json:"address"`
		State   string `json:"state"`
	}
	if err := connection.call("terminal.kill", map[string]any{"terminal": address}, &result); err != nil {
		var apiError *APIError
		if errors.As(err, &apiError) {
			return "", apiError
		}
		return "", &MutationError{Err: err, Submitted: true}
	}
	if result.Address != address {
		return "", &MutationError{Err: fmt.Errorf("Herdr Core answered the kill for %q", result.Address), Submitted: true}
	}
	return result.State, nil
}

// Run starts core's own shell in folder, labelled with the Herdr app's space
// and tab ids, and returns its address. The app adds a terminal so labelled
// when its window next opens. It is used only for New space and New terminal
// while the window is closed, with ids from a fresh read: the app adopts a
// closed space's or tab's ids by bringing it back.
func (c *CoreClient) Run(ctx context.Context, folder, space, tab string) (string, error) {
	connection, _, err := c.open(ctx)
	if err != nil {
		return "", &MutationError{Err: err}
	}
	defer connection.conn.Close()
	var result struct {
		Address string `json:"address"`
	}
	params := map[string]any{"cwd": folder, "labels": map[string]string{"herdr.space": space, "herdr.tab": tab}, "keep": true}
	if err := connection.call("terminal.run", params, &result); err != nil {
		var apiError *APIError
		if errors.As(err, &apiError) {
			return "", apiError
		}
		return "", &MutationError{Err: err, Submitted: true}
	}
	if result.Address == "" || strings.Contains(result.Address, "/") {
		return "", &MutationError{Err: fmt.Errorf("Herdr Core answered the run with %q", result.Address), Submitted: true}
	}
	return result.Address, nil
}

type CoreSubscription struct {
	conn    net.Conn
	decoder *json.Decoder
}

// Subscribe opens one plain event connection. Its reply snapshot is not used:
// Home always comes from a complete Read.
func (c *CoreClient) Subscribe(ctx context.Context) (*CoreSubscription, error) {
	connection, _, err := c.open(ctx)
	if err != nil {
		return nil, err
	}
	var started struct {
		Seq *uint64 `json:"seq"`
	}
	if err := connection.call("events.subscribe", map[string]any{}, &started); err != nil {
		connection.conn.Close()
		return nil, err
	}
	if started.Seq == nil {
		connection.conn.Close()
		return nil, errors.New("Herdr Core returned an unexpected subscription response")
	}
	return &CoreSubscription{conn: connection.conn, decoder: connection.decoder}, nil
}

// Next waits for the next notification and reports whether it changes Home.
func (s *CoreSubscription) Next() (bool, error) {
	_ = s.conn.SetReadDeadline(time.Now().Add(coreEventSilence))
	var message struct {
		Method string `json:"method"`
		Params struct {
			Type string `json:"type"`
		} `json:"params"`
	}
	if err := s.decoder.Decode(&message); err != nil {
		return false, err
	}
	switch message.Method {
	case "heartbeat":
		return false, nil
	case "event":
		return coreEventChangesHome(message.Params.Type), nil
	default:
		// "missed" and any newer notification: a complete read is always safe.
		return true, nil
	}
}

func coreEventChangesHome(kind string) bool {
	switch kind {
	case "size", "foreground", "turn", "keep":
		return false
	default:
		// created, lost, restarted, finished, removed, status, labels, title
		// and doc, plus anything newer: a complete read is always safe.
		return true
	}
}

func (s *CoreSubscription) Close() error { return s.conn.Close() }
