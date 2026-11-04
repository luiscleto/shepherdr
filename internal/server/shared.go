package server

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/luiscleto/shepherdr/internal/herdr"
	"github.com/luiscleto/shepherdr/internal/shares"
)

// sharedFilePolicy keeps a shared file's bytes from running as Shepherdr,
// whatever the file claims to be: no script, no plugin, no form, and only
// same-origin images and media.
const sharedFilePolicy = "sandbox; default-src 'none'; img-src 'self' data:; media-src 'self'; style-src 'unsafe-inline'"

const (
	maxSharedDeleteBody = 256 << 10
	maxSharedDeleteIDs  = 4096
)

// SetShares serves the files agents shared, kept in store.
func (s *Server) SetShares(store *shares.Store) {
	s.shares = store
	store.SetCurrent(s.sharedSourceCurrent)
}

// sharedSourceCurrent reports whether source's terminal is still current:
// its Herdr's Home has its pane and terminal IDs and its process is the same
// one that shared. A later terminal with the same IDs has another process.
func (s *Server) sharedSourceCurrent(source shares.Source) bool {
	_, ok := s.sharedCurrentTarget(source)
	return ok
}

func (s *Server) sharedCurrentTarget(source shares.Source) (sharedTarget, bool) {
	var projector *herdr.Projector
	switch source.Herdr {
	case string(herdr.BackendTUI):
		projector = s.projector
	case string(herdr.BackendCore):
		projector = s.coreProjector
	}
	if projector == nil {
		return sharedTarget{}, false
	}
	state := projector.Current()
	if state.Connection != herdr.ConnectionLive || state.LastKnown {
		return sharedTarget{}, false
	}
	for _, pane := range state.Snapshot.Panes {
		if pane.PaneID == source.PaneID && pane.TerminalID == source.TerminalID {
			if !shares.Running(source) {
				return sharedTarget{}, false
			}
			return sharedTarget{WorkspaceID: pane.WorkspaceID, PaneID: pane.PaneID, TerminalID: pane.TerminalID}, true
		}
	}
	return sharedTarget{}, false
}

type sharedTarget struct {
	WorkspaceID string `json:"workspace_id"`
	PaneID      string `json:"pane_id"`
	TerminalID  string `json:"terminal_id"`
}

type sharedFrom struct {
	Herdr  string `json:"herdr"`
	Handle string `json:"source"`
	Place  string `json:"place"`
	Tab    string `json:"tab,omitempty"`
	Agent  string `json:"agent,omitempty"`
	// Current is the terminal while it is still the one that shared.
	Current *sharedTarget `json:"current,omitempty"`
}

type sharedFile struct {
	ID       string      `json:"id"`
	Name     string      `json:"name"`
	Size     int64       `json:"size"`
	SharedAt int64       `json:"shared_at"`
	Seen     bool        `json:"seen"`
	Kind     shares.Kind `json:"kind"`
	Tag      string      `json:"tag"`
	From     *sharedFrom `json:"from,omitempty"`
}

// sharedMessage is the complete list, sent on the Home connection when it
// opens and whenever the list or a source terminal's currentness changes.
type sharedMessage struct {
	Type  string       `json:"type"`
	Epoch string       `json:"epoch"`
	Files []sharedFile `json:"files"`
}

func (s *Server) sharedList() sharedMessage {
	records := s.shares.List()
	files := make([]sharedFile, 0, len(records))
	for _, record := range records {
		_, kind := shares.TypeOf(record.Name)
		file := sharedFile{
			ID: record.ID, Name: record.Name, Size: record.Size, SharedAt: record.SharedAt.UnixMilli(),
			Seen: record.Seen, Kind: kind, Tag: shares.Tag(record.Name),
		}
		if source := record.Source; source != nil {
			file.From = &sharedFrom{Herdr: source.Herdr, Handle: source.Handle(), Place: source.Place, Tab: source.Tab, Agent: source.Agent}
			if target, ok := s.sharedCurrentTarget(*source); ok {
				file.From.Current = &target
			}
		}
		files = append(files, file)
	}
	return sharedMessage{Type: "shared", Epoch: s.epoch, Files: files}
}

// sharedView is the browser saying which Shared files list is in view, so
// the server can mark what it lists seen, or that none is.
type sharedView struct {
	Type       string `json:"type"`
	All        bool   `json:"all"`
	Source     string `json:"source"`
	Herdr      string `json:"herdr"`
	PaneID     string `json:"pane_id"`
	TerminalID string `json:"terminal_id"`
}

func parseSharedView(data []byte) (*shares.View, bool) {
	var message sharedView
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&message); err != nil {
		return nil, false
	}
	switch message.Type {
	case "shared.leave":
		return nil, true
	case "shared.view":
	default:
		return nil, false
	}
	if message.All {
		return &shares.View{All: true}, true
	}
	if len(message.Source) > 64 || len(message.PaneID) > 512 || len(message.TerminalID) > 512 ||
		message.Herdr != string(herdr.BackendTUI) && message.Herdr != string(herdr.BackendCore) ||
		message.Source == "" && (message.PaneID == "" || message.TerminalID == "") {
		return nil, false
	}
	return &shares.View{Handle: message.Source, Herdr: message.Herdr, PaneID: message.PaneID, TerminalID: message.TerminalID}, true
}

// readHomeMessages reads what the browser sends on the Home connection: only
// which Shared files list is in view. It returns when the connection ends.
func (s *Server) readHomeMessages(connection *websocket.Conn, request *http.Request) {
	owner := new(byte)
	if s.shares != nil {
		defer s.shares.SetView(owner, nil)
	}
	for {
		_, data, err := connection.ReadMessage()
		if err != nil {
			return
		}
		if s.shares == nil {
			continue
		}
		view, ok := parseSharedView(data)
		if !ok {
			continue
		}
		_ = withCommitAuthority(request, func() error {
			s.shares.SetView(owner, view)
			return nil
		})
	}
}

// sharedFileResponse serves one shared file's bytes, for preview or, with
// ?download=1, as an attachment. sharedFileHeaders adds the fixed headers.
func (s *Server) sharedFileResponse(writer http.ResponseWriter, request *http.Request) {
	request, lease, ok := s.bindAccessRequest(request)
	if !ok {
		writeAccessError(writer, http.StatusUnauthorized, "Sign in again.")
		return
	}
	if lease != nil {
		defer lease.Close()
		defer cleanupBoundRequest(request)
	}
	id := request.PathValue("id")
	if s.shares == nil || !shares.ValidID(id) {
		http.NotFound(writer, request)
		return
	}
	file, record, found := s.shares.Open(id)
	if !found {
		http.NotFound(writer, request)
		return
	}
	defer file.Close()
	contentType, kind := shares.TypeOf(record.Name)
	disposition := "inline"
	if kind == shares.KindOther || request.URL.Query().Get("download") == "1" {
		disposition = "attachment"
	}
	header := writer.Header()
	header.Set("Content-Type", contentType)
	if value := mime.FormatMediaType(disposition, map[string]string{"filename": record.Name}); value != "" {
		header.Set("Content-Disposition", value)
	} else {
		header.Set("Content-Disposition", disposition)
	}
	if !requestAuthorityValid(request) {
		writeAccessError(writer, http.StatusUnauthorized, "Sign in again.")
		return
	}
	// A revoked sign-in ends the response's context, and the copy stops.
	http.ServeContent(writer, request, "", record.SharedAt, &contextFile{ctx: request.Context(), file: file})
}

// sharedFilePath reports an address of one shared file's bytes.
func sharedFilePath(path string) bool {
	rest, found := strings.CutPrefix(path, "/api/shared/")
	return found && rest != "delete" && !strings.Contains(rest, "/")
}

// sharedFileWriter gives every response for a shared file's address the
// fixed confining headers, refusals and errors included. It sets them as the
// status is written, after anything else, such as ServeContent dropping
// Cache-Control on a 416, has changed them.
type sharedFileWriter struct {
	http.ResponseWriter
	wrote bool
}

func (w *sharedFileWriter) WriteHeader(status int) {
	if !w.wrote {
		w.wrote = true
		header := w.Header()
		header.Set("Content-Security-Policy", sharedFilePolicy)
		header.Set("Cross-Origin-Resource-Policy", "same-origin")
		header.Set("Cache-Control", "private, no-store")
		header.Set("X-Content-Type-Options", "nosniff")
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *sharedFileWriter) Write(data []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}

type contextFile struct {
	ctx  context.Context
	file io.ReadSeeker
}

func (c *contextFile) Read(buffer []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.file.Read(buffer)
}

func (c *contextFile) Seek(offset int64, whence int) (int64, error) {
	return c.file.Seek(offset, whence)
}

type sharedDeleteRequest struct {
	IDs []string `json:"ids"`
}

type sharedDeleteResponse struct {
	Deleted []string `json:"deleted"`
	Gone    []string `json:"gone"`
}

// sharedDelete removes exactly the listed shared files, for every device.
func (s *Server) sharedDelete(writer http.ResponseWriter, request *http.Request) {
	request, lease, ok := s.bindAccessRequest(request)
	if !ok {
		writeAccessError(writer, http.StatusUnauthorized, "Sign in again.")
		return
	}
	if lease != nil {
		defer lease.Close()
		defer cleanupBoundRequest(request)
	}
	if !exactJSONContentType(request) {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return
	}
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxSharedDeleteBody))
	decoder.DisallowUnknownFields()
	var body sharedDeleteRequest
	if err := decoder.Decode(&body); err != nil || decoder.Decode(new(any)) != io.EOF ||
		len(body.IDs) == 0 || len(body.IDs) > maxSharedDeleteIDs {
		writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
		return
	}
	for _, id := range body.IDs {
		if !shares.ValidID(id) {
			writeRefusal(writer, http.StatusBadRequest, "invalid_request", "")
			return
		}
	}
	var response sharedDeleteResponse
	var deleteErr error
	if err := withCommitAuthority(request, func() error {
		response.Deleted, response.Gone, deleteErr = s.shares.Delete(body.IDs)
		return nil
	}); err != nil {
		writeAccessError(writer, http.StatusUnauthorized, "Sign in again.")
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	if deleteErr != nil {
		writeActionJSON(writer, http.StatusInternalServerError, response)
		return
	}
	writeActionJSON(writer, http.StatusOK, response)
}

// TUIShareLookup finds a Herdr TUI pane for a share's claim with one fresh
// read: its shell's process and the labels Home shows.
func TUIShareLookup(client *herdr.Client) shares.Lookup {
	return func(ctx context.Context, paneID string) (shares.Terminal, bool, error) {
		snapshot, err := client.Snapshot(ctx)
		if err != nil {
			return shares.Terminal{}, false, err
		}
		home, err := herdr.Project(snapshot)
		if err != nil {
			return shares.Terminal{}, false, err
		}
		terminal, found := shareTerminal(home, paneID, false)
		if !found {
			return shares.Terminal{}, false, nil
		}
		pid, err := client.ShellPID(ctx, paneID)
		if err != nil {
			return shares.Terminal{}, false, err
		}
		terminal.PID = pid
		return terminal, true, nil
	}
}

// CoreShareLookup finds a Herdr Core terminal, by the address in
// HC_TERMINAL_ID, with one fresh read: its process and the labels Home shows.
func CoreShareLookup(client *herdr.CoreClient, homeFolder string) shares.Lookup {
	return func(ctx context.Context, address string) (shares.Terminal, bool, error) {
		read, err := client.Read(ctx)
		if err != nil {
			return shares.Terminal{}, false, err
		}
		_, home, err := herdr.ProjectCore(read, homeFolder)
		if err != nil {
			return shares.Terminal{}, false, err
		}
		terminal, found := shareTerminal(home, address, true)
		if !found {
			return shares.Terminal{}, false, nil
		}
		for _, candidate := range read.Terminals {
			if candidate.Address == address && candidate.PID != nil {
				terminal.PID = int(*candidate.PID)
				return terminal, true, nil
			}
		}
		return shares.Terminal{}, false, nil
	}
}

// shareTerminal is the terminal Home shows for paneID with its labels. A
// space with several tabs adds the tab, as Home lists them.
func shareTerminal(home herdr.Home, paneID string, withTab bool) (shares.Terminal, bool) {
	var search func(workspaces []herdr.Workspace) (shares.Terminal, bool)
	search = func(workspaces []herdr.Workspace) (shares.Terminal, bool) {
		for _, workspace := range workspaces {
			for _, tab := range workspace.Tabs {
				for _, terminal := range tab.Terminals {
					if terminal.PaneID != paneID {
						continue
					}
					found := shares.Terminal{PaneID: terminal.PaneID, TerminalID: terminal.TerminalID, Place: workspace.Label}
					if withTab && len(workspace.Tabs) > 1 {
						found.Tab = tab.Label
					}
					if terminal.Agent != nil {
						found.Agent = terminal.Agent.Name
					}
					return found, true
				}
			}
			if found, ok := search(workspace.Worktrees); ok {
				return found, true
			}
		}
		return shares.Terminal{}, false
	}
	return search(home.Workspaces)
}
