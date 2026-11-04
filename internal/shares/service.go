package shares

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/luiscleto/shepherdr/internal/uploads"
)

// The share protocol is JSON lines over the owner-only socket. The command
// opens with {"claim":…}, then for each file sends {"name","size"}, waits for
// {"ready":true} or an error, streams exactly size bytes and reads the result.
const (
	maxLineBytes = 64 << 10
	headerTime   = 10 * time.Second
	replyTime    = 10 * time.Second
	// A share's bytes have this long to start, then must keep arriving at
	// the minimum rate on average, as a file send from the browser must.
	paceGrace = 30 * time.Second
)

// Claim is where the command says it runs, from its environment: the Herdr
// TUI pane or the Herdr Core terminal, each with its Herdr's socket.
type Claim struct {
	TUI  *TUIClaim  `json:"tui,omitempty"`
	Core *CoreClaim `json:"core,omitempty"`
}

type TUIClaim struct {
	Socket string `json:"socket"`
	Pane   string `json:"pane"`
}

type CoreClaim struct {
	Socket   string `json:"socket"`
	Terminal string `json:"terminal"`
}

type opening struct {
	Claim Claim `json:"claim"`
}

type fileHeader struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// Reply is the server's answer to one file: ready, or the stored result,
// or an error with a message for the agent.
type Reply struct {
	Ready   bool   `json:"ready,omitempty"`
	ID      string `json:"id,omitempty"`
	Name    string `json:"name,omitempty"`
	Size    int64  `json:"size,omitempty"`
	Address string `json:"address,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Terminal is a Herdr terminal as one fresh read shows it: its process and
// the labels the Shared files page shows.
type Terminal struct {
	PID        int
	PaneID     string
	TerminalID string
	Place      string
	Tab        string
	Agent      string
}

// Lookup reads one terminal, named as the claim names it, from its Herdr.
type Lookup func(ctx context.Context, claimed string) (Terminal, bool, error)

// Herdrs are the Herdrs Shepherdr follows, for checking claims.
type Herdrs struct {
	TUISocket  string
	TUI        Lookup
	CoreSocket string
	Core       Lookup
}

// Attribute checks a claim: the claimed terminal's process, from one fresh
// read of the Herdr whose socket the claim names, must be an ancestor of the
// connecting process. A claim that fails, or none, is Outside Herdr (nil).
// The label is display text only; it never grants anything.
func (h Herdrs) Attribute(ctx context.Context, peer int, claim Claim) *Source {
	type candidate struct {
		herdr, socket, followed, id string
		lookup                      Lookup
	}
	var candidates []candidate
	if claim.TUI != nil {
		candidates = append(candidates, candidate{"tui", claim.TUI.Socket, h.TUISocket, claim.TUI.Pane, h.TUI})
	}
	if claim.Core != nil {
		candidates = append(candidates, candidate{"core", claim.Core.Socket, h.CoreSocket, claim.Core.Terminal, h.Core})
	}
	for _, c := range candidates {
		if c.lookup == nil || c.followed == "" || c.id == "" || !filepath.IsAbs(c.socket) || filepath.Clean(c.socket) != filepath.Clean(c.followed) {
			continue
		}
		lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		terminal, found, err := c.lookup(lookupCtx, c.id)
		cancel()
		if err != nil || !found || terminal.PID <= 1 {
			continue
		}
		_, start, err := processFacts(terminal.PID)
		if err != nil || !descendant(peer, terminal.PID) {
			continue
		}
		// The terminal's process must still be the one that was checked.
		if _, again, err := processFacts(terminal.PID); err != nil || again != start {
			continue
		}
		return &Source{
			Herdr: c.herdr, PaneID: terminal.PaneID, TerminalID: terminal.TerminalID,
			PID: terminal.PID, Start: start, Place: terminal.Place, Tab: terminal.Tab, Agent: terminal.Agent,
		}
	}
	return nil
}

// descendant reports whether ancestor is pid or one of its parents.
func descendant(pid, ancestor int) bool {
	for step := 0; step < 256 && pid > 1; step++ {
		if pid == ancestor {
			return true
		}
		parent, _, err := processFacts(pid)
		if err != nil || parent == pid {
			return false
		}
		pid = parent
	}
	return false
}

// Running reports whether source's terminal process is still the one that
// shared: the same process ID with the same start time.
func Running(source Source) bool {
	_, start, err := processFacts(source.PID)
	return err == nil && start == source.Start
}

// DefaultSocketPath is $XDG_RUNTIME_DIR/shepherdr/share.sock, or beside the
// access state in the user config folder when XDG_RUNTIME_DIR is unset.
func DefaultSocketPath(getenv func(string) string) (string, error) {
	if runtime := getenv("XDG_RUNTIME_DIR"); runtime != "" && filepath.IsAbs(runtime) {
		return filepath.Join(runtime, "shepherdr", "share.sock"), nil
	}
	config, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find the folder for the share socket: %w", err)
	}
	return filepath.Join(config, "shepherdr", "share.sock"), nil
}

// Listen opens the share socket. Its folder must be an owner-only 0700
// folder, not a link; Listen creates it if missing and refuses any other. A
// socket left by a stopped Shepherdr is removed only after connecting to it
// fails, so a second Shepherdr never takes over a running one's.
func Listen(path string) (*net.UnixListener, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("the share socket must be an absolute path")
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create the share socket's folder: %w", err)
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return nil, err
	}
	if !uploads.OwnerOnlyDirectory(info) {
		return nil, fmt.Errorf("the share socket's folder %s must be an owner-only 0700 folder", directory)
	}
	if existing, err := os.Lstat(path); err == nil {
		if existing.Mode()&os.ModeSocket == 0 || !uploads.OwnedByCurrentUser(existing) {
			return nil, fmt.Errorf("%s is in the way of the share socket", path)
		}
		if connection, err := net.DialTimeout("unix", path, time.Second); err == nil {
			connection.Close()
			return nil, fmt.Errorf("another Shepherdr is already sharing on %s; give this one -share-socket", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		listener.Close()
		return nil, err
	}
	return listener, nil
}

// Service receives shares on the socket into the store.
type Service struct {
	Store *Store
	// Attribute checks a connection's claim; nil makes every share Outside
	// Herdr.
	Attribute func(ctx context.Context, peer int, claim Claim) *Source
	// Origin, when set, is Shepherdr's address, printed with each file.
	Origin string
	// MinRate is the slowest average a share may arrive at, in bytes a
	// second, after its first 30 s; 0 turns the pace off.
	MinRate int64
	// Shared is told about the files one command stored, once it ends.
	Shared func([]Record)
	Logger *slog.Logger
}

// Serve accepts shares until ctx ends, then closes listener.
func (svc *Service) Serve(ctx context.Context, listener *net.UnixListener) {
	var connections sync.WaitGroup
	stop := context.AfterFunc(ctx, func() { listener.Close() })
	defer stop()
	for {
		connection, err := listener.AcceptUnix()
		if err != nil {
			if ctx.Err() == nil && !errors.Is(err, net.ErrClosed) {
				svc.Logger.Error("The share socket stopped", "error", err)
			}
			break
		}
		connections.Add(1)
		go func() {
			defer connections.Done()
			svc.handle(ctx, connection)
		}()
	}
	connections.Wait()
}

func (svc *Service) handle(ctx context.Context, connection *net.UnixConn) {
	defer connection.Close()
	stop := context.AfterFunc(ctx, func() { _ = connection.SetDeadline(time.Now()) })
	defer stop()
	peer, uid, err := peerCredentials(connection)
	if err != nil || uid != os.Geteuid() {
		return
	}
	paced := &pacedConnection{connection: connection, rate: svc.MinRate, start: time.Now()}
	reader := bufio.NewReaderSize(paced, copyBufferSize)
	writer := json.NewEncoder(connection)
	reply := func(value Reply) error {
		_ = connection.SetWriteDeadline(time.Now().Add(replyTime))
		return writer.Encode(value)
	}
	paced.headerDeadline()
	var open opening
	if err := readLine(reader, &open); err != nil {
		return
	}
	var source *Source
	if svc.Attribute != nil {
		source = svc.Attribute(ctx, peer, open.Claim)
	}
	var stored []Record
	defer func() {
		if len(stored) > 0 && svc.Shared != nil {
			svc.Shared(stored)
		}
	}()
	for {
		paced.headerDeadline()
		var header fileHeader
		if err := readLine(reader, &header); err != nil {
			return
		}
		incoming, err := svc.Store.Begin(header.Name, header.Size)
		if err != nil {
			if reply(Reply{Error: shareRefusal(err, svc.Store)}) != nil {
				return
			}
			continue
		}
		if reply(Reply{Ready: true}) != nil {
			incoming.Abort()
			return
		}
		paced.pace()
		if err := incoming.Copy(ctx, reader); err != nil {
			incoming.Abort()
			return
		}
		record, err := incoming.Commit(source)
		if err != nil {
			svc.Logger.Error("A shared file couldn't be kept", "error", err)
			_ = reply(Reply{Error: "Shepherdr couldn't keep the file. Try again."})
			return
		}
		stored = append(stored, record)
		address := ""
		if svc.Origin != "" {
			address = svc.Origin + "/#shared=" + record.ID
		}
		if reply(Reply{ID: record.ID, Name: record.Name, Size: record.Size, Address: address}) != nil {
			return
		}
	}
}

func shareRefusal(err error, store *Store) string {
	var full *FullError
	switch {
	case errors.Is(err, ErrTooLarge):
		return fmt.Sprintf("it's larger than %s, the most Shepherdr keeps for one file", formatLimit(store.limit))
	case errors.As(err, &full):
		return fmt.Sprintf("shared files are full: %s of %s kept. Ask the person to delete some in Shepherdr, then share again",
			FormatSize(full.Kept), FormatSize(full.Total))
	default:
		return "Shepherdr couldn't store it"
	}
}

func formatLimit(limit uploads.Limit) string {
	return FormatSize(limit.Bytes)
}

func readLine(reader *bufio.Reader, value any) error {
	line, err := reader.ReadSlice('\n')
	if err != nil {
		return err
	}
	if len(line) > maxLineBytes {
		return errors.New("line too long")
	}
	return json.Unmarshal(line, value)
}

// pacedConnection gives the opening and each file header a short deadline,
// and a file's bytes the upload pace: paceGrace to start, then the minimum
// average rate.
type pacedConnection struct {
	connection *net.UnixConn
	rate       int64
	start      time.Time
	read       int64
	pacing     bool
}

func (p *pacedConnection) headerDeadline() {
	p.pacing = false
	_ = p.connection.SetReadDeadline(time.Now().Add(headerTime))
}

func (p *pacedConnection) pace() {
	p.pacing = true
	p.start = time.Now()
	p.read = 0
	if p.rate == 0 {
		_ = p.connection.SetReadDeadline(time.Time{})
	}
}

func (p *pacedConnection) Read(buffer []byte) (int, error) {
	if p.pacing && p.rate > 0 {
		_ = p.connection.SetReadDeadline(p.start.Add(paceGrace + time.Duration(p.read/p.rate)*time.Second))
	}
	count, err := p.connection.Read(buffer)
	p.read += int64(count)
	if errors.Is(err, syscall.ECONNRESET) {
		err = io.ErrUnexpectedEOF
	}
	return count, err
}
