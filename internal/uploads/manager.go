package uploads

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/luiscleto/shepherdr/internal/herdr"
)

const markerName = ".shepherdr-upload"

// File is one file of a send: its name and its Size bytes, read from Data
// as they are written to the staged file.
type File struct {
	Data io.Reader
	Name string
	Size int64
}

type ForwardResult string

const (
	Forwarded ForwardResult = "forwarded"
	NotSent   ForwardResult = "not_sent"
	Occupied  ForwardResult = "occupied"
	Unknown   ForwardResult = "unknown"
)

type association struct {
	Directory   string `json:"directory"`
	Ownership   string `json:"ownership"`
	WorkspaceID string `json:"workspace_id"`
}

// workspaceGate keeps a workspace's folder for its sends. Sends hold it
// shared, so one long send doesn't hold up the next; cleanup holds it alone,
// so a folder never goes while a send is using it. setup guards the other
// fields while sends share the gate.
type workspaceGate struct {
	mu          sync.RWMutex
	setup       sync.Mutex
	association *association
	directory   *ownedDirectory
	// open ends, with its cause, when cleanup or Close begins, and so stops
	// the gate's sends instead of waiting for them.
	open context.Context
	end  context.CancelCauseFunc
}

var (
	errWorkspaceClosed = errors.New("the workspace closed during the send")
	errStopping        = errors.New("Shepherdr is stopping")
)

func newWorkspaceGate(record *association) *workspaceGate {
	gate := &workspaceGate{association: record}
	gate.open, gate.end = context.WithCancelCause(context.Background())
	return gate
}

// stopSends ends the gate's current sends, and any that take the gate before
// reopen, with cause.
func (gate *workspaceGate) stopSends(cause error) {
	gate.setup.Lock()
	defer gate.setup.Unlock()
	gate.end(cause)
}

func (gate *workspaceGate) reopen() {
	gate.setup.Lock()
	defer gate.setup.Unlock()
	gate.open, gate.end = context.WithCancelCause(context.Background())
}

// release ends one send's use of directory. A handle that is no longer the
// gate's folder closes when its last send ends, so a send never loses the
// handle it writes and rolls back through.
func (gate *workspaceGate) release(directory *ownedDirectory) {
	gate.setup.Lock()
	defer gate.setup.Unlock()
	directory.users--
	if directory.users == 0 && directory != gate.directory {
		closeOwnedDirectory(directory)
	}
}

type Manager struct {
	logger    *slog.Logger
	root      *os.Root
	rootPath  string
	statePath string

	gatesMu sync.Mutex
	gates   map[string]*workspaceGate

	recordsMu sync.Mutex
	records   map[string]association

	observationMu sync.Mutex
	closed        bool
	observed      bool
	workspaces    map[string]struct{}

	cleanup sync.WaitGroup
}

func DefaultStatePath() (string, error) {
	directory, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find user configuration directory: %w", err)
	}
	return filepath.Join(directory, "shepherdr", "uploads.json"), nil
}

func Open(root, statePath string, logger *slog.Logger) (*Manager, error) {
	if logger == nil {
		logger = slog.Default()
	}
	resolvedRoot, err := prepareRoot(root)
	if err != nil {
		return nil, err
	}
	rootHandle, err := openStableRoot(resolvedRoot)
	if err != nil {
		return nil, err
	}
	records, err := readAssociations(statePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		rootHandle.Close()
		return nil, err
	}
	manager := &Manager{
		gates: make(map[string]*workspaceGate), logger: logger, records: records,
		root: rootHandle, rootPath: resolvedRoot, statePath: statePath, workspaces: make(map[string]struct{}),
	}
	for workspaceID, record := range records {
		copy := record
		manager.gates[workspaceID] = newWorkspaceGate(&copy)
	}
	return manager, nil
}

func openStableRoot(path string) (*os.Root, error) {
	before, err := os.Lstat(path)
	if err != nil || !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("upload parent must be a direct directory")
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, fmt.Errorf("open upload parent: %w", err)
	}
	after, err := root.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		root.Close()
		return nil, errors.New("upload parent changed while it was opened")
	}
	return root, nil
}

func prepareRoot(root string) (string, error) {
	if root == "" {
		return "", errors.New("upload parent is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve upload parent: %w", err)
	}
	absolute = filepath.Clean(absolute)
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return "", fmt.Errorf("create upload parent: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve upload parent links: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", errors.New("upload parent must be a directory")
	}
	if err := ValidatePathText(resolved); err != nil {
		return "", fmt.Errorf("upload parent cannot be used in terminal text: %w", err)
	}
	return filepath.Clean(resolved), nil
}

func (m *Manager) gate(workspaceID string) *workspaceGate {
	m.gatesMu.Lock()
	defer m.gatesMu.Unlock()
	gate := m.gates[workspaceID]
	if gate == nil {
		gate = newWorkspaceGate(nil)
		m.gates[workspaceID] = gate
	}
	return gate
}

// StageAndForward holds the one stable workspace gate, shared with other
// sends, across validation, staging, and the terminal result. Files are
// written as their bytes arrive. Definite pre-forward outcomes remove only
// files created by this request. An unknown result retains them because their
// paths may already be in the terminal.
//
// If the workspace closes or the manager closes before forwarding, the send
// stops: interrupt, when given, is called once from another goroutine and
// must make waiting reads of the files' data fail at once. It never runs
// after StageAndForward returns, so it may use the request's connection.
func (m *Manager) StageAndForward(
	ctx context.Context,
	workspaceID, workspaceLabel string,
	files []File,
	interrupt func(),
	validate func() error,
	forward func([]string, func() error) (ForwardResult, error),
) (ForwardResult, error) {
	if !validIdentity(workspaceID) {
		return NotSent, errors.New("workspace is invalid")
	}
	gate := m.gate(workspaceID)
	gate.mu.RLock()
	defer gate.mu.RUnlock()
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	gate.setup.Lock()
	open := gate.open
	gate.setup.Unlock()
	defer context.AfterFunc(open, func() { cancel(context.Cause(open)) })()
	if interrupt != nil {
		interrupted := make(chan struct{})
		stopInterrupt := context.AfterFunc(ctx, func() {
			defer close(interrupted)
			interrupt()
		})
		defer func() {
			if !stopInterrupt() {
				<-interrupted
			}
		}()
	}
	if ctx.Err() != nil {
		return NotSent, context.Cause(ctx)
	}
	if err := validate(); err != nil {
		return NotSent, err
	}
	gate.setup.Lock()
	directory, err := m.ensureAssociation(gate, workspaceID, workspaceLabel)
	if err == nil {
		directory.users++
	}
	gate.setup.Unlock()
	if err != nil {
		return NotSent, err
	}
	defer gate.release(directory)
	staged, err := stageFiles(ctx, directory, files)
	if err != nil {
		return NotSent, err
	}
	paths := make([]string, len(staged))
	for index := range staged {
		paths[index] = staged[index].path
	}
	validateStaged := func() error {
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}
		present, err := verifyOwnedDirectory(directory)
		if err != nil {
			return err
		}
		if !present {
			return errors.New("upload directory disappeared")
		}
		return verifyPathBinding(directory)
	}
	if err := validateStaged(); err != nil {
		return NotSent, rollbackStageFiles(directory.root, staged, err)
	}
	result, forwardErr := forward(paths, validateStaged)
	if result == NotSent || result == Occupied {
		if rollbackErr := removeRequestFiles(directory.root, staged); rollbackErr != nil {
			m.logger.Error("upload rollback was incomplete", "workspace", workspaceID, "error", rollbackErr)
		}
	}
	return result, forwardErr
}

func (m *Manager) ensureAssociation(gate *workspaceGate, workspaceID, workspaceLabel string) (*ownedDirectory, error) {
	if gate.directory != nil {
		present, err := verifyOwnedDirectory(gate.directory)
		if err == nil && present {
			return gate.directory, nil
		}
		if err != nil {
			m.logger.Error("recorded upload directory was refused", "workspace", workspaceID, "error", err)
		}
		if gate.directory.users == 0 {
			closeOwnedDirectory(gate.directory)
		}
		gate.directory = nil
	} else if gate.association != nil {
		directory, present, err := openOwnedDirectory(*gate.association)
		if err == nil && present {
			gate.directory = directory
			return gate.directory, nil
		}
		if err != nil {
			m.logger.Error("recorded upload directory was refused", "workspace", workspaceID, "error", err)
		}
	}
	directory, err := createAssociation(m.root, m.rootPath, workspaceID, workspaceLabel)
	if err != nil {
		return nil, err
	}
	if err := m.replaceRecord(workspaceID, &directory.record); err != nil {
		if rollbackErr := removeOwnedDirectory(directory, false); rollbackErr != nil {
			m.logger.Error("upload association rollback was incomplete", "workspace", workspaceID, "error", rollbackErr)
		}
		closeOwnedDirectory(directory)
		return nil, err
	}
	gate.association = &directory.record
	gate.directory = directory
	return gate.directory, nil
}

func (m *Manager) replaceRecord(workspaceID string, record *association) error {
	m.recordsMu.Lock()
	defer m.recordsMu.Unlock()
	updated := make(map[string]association, len(m.records)+1)
	for id, existing := range m.records {
		updated[id] = existing
	}
	if record == nil {
		delete(updated, workspaceID)
	} else {
		updated[workspaceID] = *record
	}
	if err := writeAssociations(m.statePath, updated); err != nil {
		return err
	}
	m.records = updated
	return nil
}

func (m *Manager) cleanupWorkspace(workspaceID string) error {
	gate := m.gate(workspaceID)
	gate.stopSends(errWorkspaceClosed)
	gate.mu.Lock()
	defer gate.mu.Unlock()
	defer gate.reopen()
	if gate.association == nil {
		return nil
	}
	directory := gate.directory
	if directory == nil {
		var err error
		var present bool
		directory, present, err = openOwnedDirectory(*gate.association)
		if err != nil {
			return err
		}
		if !present {
			if err := m.replaceRecord(workspaceID, nil); err != nil {
				return err
			}
			gate.association = nil
			return nil
		}
		gate.directory = directory
	}
	if err := removeOwnedDirectory(directory, true); err != nil {
		return fmt.Errorf("remove owned upload directory: %w", err)
	}
	closeOwnedDirectory(directory)
	gate.directory = nil
	if err := m.replaceRecord(workspaceID, nil); err != nil {
		return err
	}
	gate.association = nil
	return nil
}

// ObservePublishedSnapshot is called only after the complete state has been
// projected and published. It hands each missing association or
// present-to-absent workspace transition to one bounded cleanup call.
func (m *Manager) ObservePublishedSnapshot(snapshot herdr.Snapshot, _ bool) {
	current := make(map[string]struct{}, len(snapshot.Workspaces))
	for _, workspace := range snapshot.Workspaces {
		current[workspace.WorkspaceID] = struct{}{}
	}
	m.observationMu.Lock()
	if m.closed {
		m.observationMu.Unlock()
		return
	}
	missing := make([]string, 0)
	if !m.observed {
		m.recordsMu.Lock()
		for workspaceID := range m.records {
			if _, present := current[workspaceID]; !present {
				missing = append(missing, workspaceID)
			}
		}
		m.recordsMu.Unlock()
		m.observed = true
	} else {
		for workspaceID := range m.workspaces {
			if _, present := current[workspaceID]; !present {
				missing = append(missing, workspaceID)
			}
		}
	}
	m.workspaces = current
	m.cleanup.Add(len(missing))
	m.observationMu.Unlock()
	for _, workspaceID := range missing {
		workspaceID := workspaceID
		go func() {
			defer m.cleanup.Done()
			if err := m.cleanupWorkspace(workspaceID); err != nil {
				m.logger.Error("workspace upload cleanup failed", "workspace", workspaceID, "error", err)
			}
		}()
	}
}

func (m *Manager) Close() {
	m.observationMu.Lock()
	if m.closed {
		m.observationMu.Unlock()
		return
	}
	m.closed = true
	m.observationMu.Unlock()
	m.cleanup.Wait()
	m.gatesMu.Lock()
	gates := make([]*workspaceGate, 0, len(m.gates))
	for _, gate := range m.gates {
		gates = append(gates, gate)
	}
	m.gatesMu.Unlock()
	for _, gate := range gates {
		gate.stopSends(errStopping)
	}
	for _, gate := range gates {
		gate.mu.Lock()
		closeOwnedDirectory(gate.directory)
		gate.directory = nil
		gate.mu.Unlock()
	}
	_ = m.root.Close()
}

func createAssociation(parent *os.Root, rootPath, workspaceID, label string) (*ownedDirectory, error) {
	for {
		ownershipBytes := make([]byte, 16)
		if _, err := rand.Read(ownershipBytes); err != nil {
			return nil, fmt.Errorf("create upload ownership value: %w", err)
		}
		ownership := hex.EncodeToString(ownershipBytes)
		name := "shepherdr-" + safeLabel(label) + "-" + ownership
		if err := parent.Mkdir(name, 0o700); err != nil {
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return nil, fmt.Errorf("create workspace upload directory: %w", err)
		}
		directory, err := openCreatedDirectory(parent, rootPath, name, association{
			Directory: filepath.Join(rootPath, name), Ownership: ownership, WorkspaceID: workspaceID,
		})
		if err != nil {
			if rollbackErr := parent.RemoveAll(name); rollbackErr != nil {
				return nil, errors.Join(err, fmt.Errorf("upload association rollback was incomplete: %w", rollbackErr))
			}
			return nil, err
		}
		if err := writeMarker(directory); err != nil {
			if rollbackErr := removeOwnedDirectory(directory, false); rollbackErr != nil {
				err = errors.Join(err, fmt.Errorf("upload association rollback was incomplete: %w", rollbackErr))
			}
			closeOwnedDirectory(directory)
			return nil, err
		}
		return directory, nil
	}
}

func safeLabel(label string) string {
	var builder strings.Builder
	for _, character := range label {
		if builder.Len() >= 32 {
			break
		}
		switch {
		case unicode.IsLetter(character) || unicode.IsDigit(character):
			if builder.Len()+utf8.RuneLen(character) <= 32 {
				builder.WriteRune(character)
			}
		case character == '-' || character == '_':
			builder.WriteRune(character)
		case unicode.IsSpace(character) && builder.Len() > 0:
			builder.WriteByte('-')
		}
	}
	value := strings.Trim(builder.String(), "-_")
	if value == "" {
		return "workspace"
	}
	return value
}

func validIdentity(value string) bool {
	if value == "" || len(value) > 512 || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if unsafePathRune(character) {
			return false
		}
	}
	return true
}
