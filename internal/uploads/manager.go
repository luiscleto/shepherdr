package uploads

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/luisc/shepherdr/internal/herdr"
)

const markerName = ".shepherdr-upload"

type File struct {
	Data []byte
	Name string
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

type workspaceGate struct {
	mu          sync.Mutex
	association *association
}

type Manager struct {
	logger    *slog.Logger
	root      string
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
	records, err := readAssociations(statePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	manager := &Manager{
		gates: make(map[string]*workspaceGate), logger: logger, records: records,
		root: resolvedRoot, statePath: statePath, workspaces: make(map[string]struct{}),
	}
	for workspaceID, record := range records {
		copy := record
		manager.gates[workspaceID] = &workspaceGate{association: &copy}
	}
	return manager, nil
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
		gate = &workspaceGate{}
		m.gates[workspaceID] = gate
	}
	return gate
}

// StageAndForward holds the one stable workspace gate across validation,
// staging, and the terminal result. Definite pre-forward outcomes remove only
// files created by this request. An unknown result retains them because their
// paths may already be in the terminal.
func (m *Manager) StageAndForward(
	ctx context.Context,
	workspaceID, workspaceLabel string,
	files []File,
	validate func() error,
	forward func([]string) (ForwardResult, error),
) (ForwardResult, error) {
	if !validIdentity(workspaceID) {
		return NotSent, errors.New("workspace is invalid")
	}
	gate := m.gate(workspaceID)
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return NotSent, err
	}
	if err := validate(); err != nil {
		return NotSent, err
	}
	record, err := m.ensureAssociation(gate, workspaceID, workspaceLabel)
	if err != nil {
		return NotSent, err
	}
	paths, err := stageFiles(ctx, *record, files)
	if err != nil {
		return NotSent, err
	}
	result, forwardErr := forward(paths)
	if result == NotSent || result == Occupied {
		if rollbackErr := removeRequestFiles(paths); rollbackErr != nil {
			m.logger.Error("upload rollback was incomplete", "workspace", workspaceID, "error", rollbackErr)
		}
	}
	return result, forwardErr
}

func (m *Manager) ensureAssociation(gate *workspaceGate, workspaceID, workspaceLabel string) (*association, error) {
	if gate.association != nil {
		present, err := verifyAssociation(*gate.association)
		if err == nil && present {
			return gate.association, nil
		}
		if err != nil {
			m.logger.Error("recorded upload directory was refused", "workspace", workspaceID, "error", err)
		}
	}
	record, err := createAssociation(m.root, workspaceID, workspaceLabel)
	if err != nil {
		return nil, err
	}
	if err := m.replaceRecord(workspaceID, &record); err != nil {
		if rollbackErr := os.RemoveAll(record.Directory); rollbackErr != nil {
			m.logger.Error("upload association rollback was incomplete", "workspace", workspaceID, "error", rollbackErr)
		}
		return nil, err
	}
	gate.association = &record
	return gate.association, nil
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
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.association == nil {
		return nil
	}
	present, err := verifyAssociation(*gate.association)
	if err != nil {
		return err
	}
	if present {
		if err := os.RemoveAll(gate.association.Directory); err != nil {
			return fmt.Errorf("remove owned upload directory: %w", err)
		}
	}
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
	m.closed = true
	m.observationMu.Unlock()
	m.cleanup.Wait()
}

func createAssociation(root, workspaceID, label string) (association, error) {
	for {
		ownershipBytes := make([]byte, 16)
		if _, err := rand.Read(ownershipBytes); err != nil {
			return association{}, fmt.Errorf("create upload ownership value: %w", err)
		}
		ownership := hex.EncodeToString(ownershipBytes)
		directory := filepath.Join(root, "shepherdr-"+safeLabel(label)+"-"+ownership)
		if err := os.Mkdir(directory, 0o700); err != nil {
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return association{}, fmt.Errorf("create workspace upload directory: %w", err)
		}
		record := association{Directory: directory, Ownership: ownership, WorkspaceID: workspaceID}
		if err := writeMarker(record); err != nil {
			if rollbackErr := os.RemoveAll(directory); rollbackErr != nil {
				return association{}, errors.Join(err, fmt.Errorf("upload association rollback was incomplete: %w", rollbackErr))
			}
			return association{}, err
		}
		return record, nil
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
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}
