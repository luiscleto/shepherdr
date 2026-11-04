package shares

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/luiscleto/shepherdr/internal/uploads"
)

const (
	indexName      = "shared.json"
	indexTemporary = ".shared-" // starts the name of an index being written
	incomingName   = ".incoming"
	indexVersion   = 1
	maxIndexBytes  = 16 << 20
	maxNameBytes   = 200
	DefaultLimit   = int64(1 << 30)
	DefaultTotal   = int64(10 << 30)
	idBytes        = 16
	copyBufferSize = 64 << 10
)

var (
	ErrTooLarge = errors.New("file is larger than the limit for one shared file")
	ErrFull     = errors.New("shared files are full")
	// ErrInUse is another Shepherdr already keeping shared files in the
	// folder. Open leaves the folder untouched.
	ErrInUse = errors.New("another Shepherdr already keeps shared files")
)

// FullError says how much is kept when a share would pass the total limit.
type FullError struct {
	Kept, Total int64
}

func (e *FullError) Error() string { return ErrFull.Error() }
func (e *FullError) Unwrap() error { return ErrFull }

// View is a Shared files list in view on some device. Files it lists become
// seen. All is the whole list; otherwise it is one terminal's files, named by
// Handle once a file from it is known, else by its current IDs.
type View struct {
	All        bool
	Handle     string
	Herdr      string
	PaneID     string
	TerminalID string
}

func (v View) matches(record Record, current func(Source) bool) bool {
	if v.All {
		return true
	}
	if record.Source == nil {
		return false
	}
	if v.Handle != "" && record.Source.Handle() == v.Handle {
		return true
	}
	return v.PaneID != "" && record.Source.Herdr == v.Herdr && record.Source.PaneID == v.PaneID &&
		record.Source.TerminalID == v.TerminalID && current(*record.Source)
}

// Store keeps shared files in an owner-only folder: one 0700 subfolder per
// file, named by a random ID, and the index beside them. It holds an
// exclusive lock on the folder while open, so nothing else writes there and
// startup can clear what an interrupted share left behind.
type Store struct {
	dir    string
	root   *os.Root
	lock   *os.File
	limit  uploads.Limit
	total  uploads.Limit
	logger *slog.Logger

	mu          sync.Mutex
	records     []Record
	kept        int64
	reserved    int64
	views       map[any]View
	current     func(Source) bool
	subscribers map[chan struct{}]struct{}
}

// DefaultDirectory is $XDG_DATA_HOME/shepherdr/shared, else
// ~/.local/share/shepherdr/shared, or the user config folder on macOS when
// neither is set.
func DefaultDirectory(getenv func(string) string, goos string) (string, error) {
	if data := getenv("XDG_DATA_HOME"); data != "" && filepath.IsAbs(data) {
		return filepath.Join(data, "shepherdr", "shared"), nil
	}
	if goos == "darwin" {
		config, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(config, "shepherdr", "shared"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find the home folder for shared files: %w", err)
	}
	return filepath.Join(home, ".local", "share", "shepherdr", "shared"), nil
}

// Open opens or creates the store in dir and locks it for this process. It
// clears unfinished shares, drops index entries whose file is gone and
// removes folders the index doesn't name. If another process holds the
// folder it returns ErrInUse and changes nothing.
func Open(dir string, limit, total uploads.Limit, logger *slog.Logger) (*Store, error) {
	if !filepath.IsAbs(dir) {
		return nil, errors.New("the shared files folder must be an absolute path")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create the shared files folder: %w", err)
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !uploads.OwnerOnlyDirectory(info) {
		return nil, fmt.Errorf("the shared files folder %s must be an owner-only 0700 folder", dir)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	lock, err := root.Open(".")
	if err != nil {
		root.Close()
		return nil, err
	}
	opened, err := lock.Stat()
	if err != nil || !os.SameFile(info, opened) {
		lock.Close()
		root.Close()
		return nil, errors.New("the shared files folder changed while it was opened")
	}
	// Two Shepherdrs on one folder would each keep their own list and drop
	// the other's files.
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		root.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("%w in %s; give this one -share-dir", ErrInUse, dir)
		}
		return nil, fmt.Errorf("lock the shared files folder: %w", err)
	}
	store := &Store{
		dir: dir, root: root, lock: lock, limit: limit, total: total, logger: logger,
		views: map[any]View{}, subscribers: map[chan struct{}]struct{}{},
		current: func(Source) bool { return false },
	}
	if err := store.load(); err != nil {
		store.Close()
		return nil, err
	}
	return store, nil
}

// Close releases the folder.
func (s *Store) Close() error {
	return errors.Join(s.root.Close(), s.lock.Close())
}

// SetCurrent sets how the store tells whether a source terminal is still
// current, for views named by terminal IDs.
func (s *Store) SetCurrent(current func(Source) bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.current = current
}

func (s *Store) load() error {
	records, err := s.readIndex()
	if err != nil {
		return err
	}
	if err := s.root.RemoveAll(incomingName); err != nil {
		return fmt.Errorf("clear unfinished shares: %w", err)
	}
	if err := s.root.Mkdir(incomingName, 0o700); err != nil {
		return fmt.Errorf("create the folder for incoming shares: %w", err)
	}
	kept := records[:0]
	known := map[string]bool{}
	for _, record := range records {
		if s.present(record) {
			kept = append(kept, record)
			known[record.ID] = true
			continue
		}
		s.logger.Warn("A shared file is missing, so it was removed from the list", "id", record.ID)
		_ = s.root.RemoveAll(record.ID)
	}
	entries, err := fs.ReadDir(s.root.FS(), ".")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		// An index write that stopped before its rename left its temporary
		// copy.
		if strings.HasPrefix(entry.Name(), indexTemporary) {
			if err := s.root.Remove(entry.Name()); err != nil {
				return err
			}
			continue
		}
		if validID(entry.Name()) && !known[entry.Name()] {
			s.logger.Warn("Removed a shared file that was never listed", "id", entry.Name())
			if err := s.root.RemoveAll(entry.Name()); err != nil {
				return err
			}
		}
	}
	changed := len(kept) != len(records)
	s.records = kept
	for _, record := range kept {
		s.kept += record.Size
	}
	if changed {
		return s.writeIndex()
	}
	return nil
}

func (s *Store) present(record Record) bool {
	info, err := s.root.Lstat(record.ID)
	if err != nil || !uploads.OwnerOnlyDirectory(info) {
		return false
	}
	file, err := uploads.OpenPrivateRegularAt(s.root, filepath.Join(record.ID, record.Name), "shared file")
	if err != nil {
		return false
	}
	defer file.Close()
	stat, err := file.Stat()
	return err == nil && stat.Size() == record.Size
}

type index struct {
	Files   []Record `json:"files"`
	Version int      `json:"version"`
}

func (s *Store) readIndex() ([]Record, error) {
	file, err := uploads.OpenPrivateRegular(filepath.Join(s.dir, indexName), "the shared files index")
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxIndexBytes+1))
	if err != nil || len(data) > maxIndexBytes {
		return nil, errors.New("the shared files index can't be read within its size bound")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var value index
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode the shared files index: %w", err)
	}
	if value.Version != indexVersion {
		return nil, fmt.Errorf("shared files index version %d is not supported", value.Version)
	}
	seen := map[string]bool{}
	for _, record := range value.Files {
		if !validID(record.ID) || seen[record.ID] || record.Name != safeName(record.Name) || record.Size < 0 {
			return nil, errors.New("the shared files index has an invalid entry")
		}
		seen[record.ID] = true
	}
	return value.Files, nil
}

// writeIndex replaces the index atomically. Call with s.mu held, or before
// the store is shared.
func (s *Store) writeIndex() error {
	data, err := json.MarshalIndent(index{Files: s.records, Version: indexVersion}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(s.dir, indexTemporary+"*")
	if err != nil {
		return fmt.Errorf("write the shared files index: %w", err)
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, filepath.Join(s.dir, indexName)); err != nil {
		return err
	}
	return syncDirectory(s.root, ".")
}

func syncDirectory(root *os.Root, name string) error {
	directory, err := root.Open(name)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

// List is every shared file, newest last.
func (s *Store) List() []Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.records)
}

// Subscribe signals on the returned channel after each change to the list.
func (s *Store) Subscribe() (<-chan struct{}, func()) {
	changes := make(chan struct{}, 1)
	s.mu.Lock()
	s.subscribers[changes] = struct{}{}
	s.mu.Unlock()
	return changes, func() {
		s.mu.Lock()
		delete(s.subscribers, changes)
		s.mu.Unlock()
	}
}

func (s *Store) notifyLocked() {
	for subscriber := range s.subscribers {
		select {
		case subscriber <- struct{}{}:
		default:
		}
	}
}

// Changed tells subscribers to read the list again, such as when a source
// terminal's currentness may have changed.
func (s *Store) Changed() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notifyLocked()
}

// SetView records that owner shows view, marking the files it lists seen,
// and keeps marking files that arrive while it stays. A nil view clears it.
func (s *Store) SetView(owner any, view *View) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if view == nil {
		delete(s.views, owner)
		return
	}
	s.views[owner] = *view
	changed := false
	for index, record := range s.records {
		if !record.Seen && view.matches(record, s.current) {
			s.records[index].Seen = true
			changed = true
		}
	}
	if !changed {
		return
	}
	if err := s.writeIndex(); err != nil {
		s.logger.Error("Couldn't save which shared files were seen", "error", err)
	}
	s.notifyLocked()
}

// Open opens a shared file's copy for reading.
func (s *Store) Open(id string) (*os.File, Record, bool) {
	s.mu.Lock()
	index := slices.IndexFunc(s.records, func(record Record) bool { return record.ID == id })
	if index < 0 {
		s.mu.Unlock()
		return nil, Record{}, false
	}
	record := s.records[index]
	s.mu.Unlock()
	file, err := uploads.OpenPrivateRegularAt(s.root, filepath.Join(record.ID, record.Name), "shared file")
	if err != nil {
		return nil, Record{}, false
	}
	return file, record, true
}

// Delete removes exactly the listed files. Those no longer listed are gone.
func (s *Store) Delete(ids []string) (deleted, gone []string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	deleted, gone = []string{}, []string{}
	for _, id := range ids {
		index := slices.IndexFunc(s.records, func(record Record) bool { return record.ID == id })
		if index < 0 {
			gone = append(gone, id)
			continue
		}
		if removeErr := s.root.RemoveAll(id); removeErr != nil {
			err = errors.Join(err, removeErr)
			continue
		}
		s.kept -= s.records[index].Size
		s.records = slices.Delete(s.records, index, index+1)
		deleted = append(deleted, id)
	}
	if len(deleted) > 0 {
		if writeErr := s.writeIndex(); writeErr != nil {
			err = errors.Join(err, writeErr)
		}
		s.notifyLocked()
	}
	return deleted, gone, err
}

// Incoming is a share being received: its bytes go to .incoming until
// Commit moves them into their own folder and lists them.
type Incoming struct {
	store *Store
	id    string
	name  string
	size  int64
	file  *os.File
	done  bool
}

// Begin reserves room for a file of size bytes, refusing one that would pass
// either limit before any byte is stored.
func (s *Store) Begin(name string, size int64) (*Incoming, error) {
	if size < 0 {
		return nil, errors.New("the file size is invalid")
	}
	s.mu.Lock()
	if !s.limit.Allows(size) {
		s.mu.Unlock()
		return nil, ErrTooLarge
	}
	if !s.total.Unlimited && (size > s.total.Bytes || s.kept+s.reserved > s.total.Bytes-size) {
		kept := s.kept
		s.mu.Unlock()
		return nil, &FullError{Kept: kept, Total: s.total.Bytes}
	}
	s.reserved += size
	s.mu.Unlock()
	incoming := &Incoming{store: s, id: newID(), name: safeName(name), size: size}
	file, err := s.root.OpenFile(filepath.Join(incomingName, incoming.id), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		incoming.release()
		return nil, fmt.Errorf("store the shared file: %w", err)
	}
	incoming.file = file
	return incoming, nil
}

// Copy writes exactly the file's size from data, as it arrives.
func (in *Incoming) Copy(ctx context.Context, data io.Reader) error {
	buffer := make([]byte, copyBufferSize)
	remaining := in.size
	for remaining > 0 {
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}
		count, err := data.Read(buffer[:min(int64(len(buffer)), remaining)])
		if count > 0 {
			if _, writeErr := in.file.Write(buffer[:count]); writeErr != nil {
				return writeErr
			}
			remaining -= int64(count)
		}
		if remaining > 0 && errors.Is(err, io.EOF) {
			return io.ErrUnexpectedEOF
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
	}
	return nil
}

// Commit lists the received file as shared from source, nil for Outside
// Herdr. A view showing its list marks it seen at once.
func (in *Incoming) Commit(source *Source) (Record, error) {
	s := in.store
	if err := in.file.Sync(); err != nil {
		in.Abort()
		return Record{}, err
	}
	if err := in.file.Close(); err != nil {
		in.file = nil
		in.Abort()
		return Record{}, err
	}
	in.file = nil
	if err := s.root.Mkdir(in.id, 0o700); err != nil {
		in.Abort()
		return Record{}, err
	}
	if err := s.root.Rename(filepath.Join(incomingName, in.id), filepath.Join(in.id, in.name)); err != nil {
		_ = s.root.RemoveAll(in.id)
		in.Abort()
		return Record{}, err
	}
	record := Record{ID: in.id, Name: in.name, Size: in.size, SharedAt: time.Now().UTC().Truncate(time.Millisecond), Source: source}
	s.mu.Lock()
	defer s.mu.Unlock()
	in.done = true
	s.reserved -= in.size
	for _, view := range s.views {
		// A new file's source was just checked, so it is current.
		if view.matches(record, func(Source) bool { return true }) {
			record.Seen = true
			break
		}
	}
	s.records = append(s.records, record)
	s.kept += record.Size
	if err := s.writeIndex(); err != nil {
		s.records = s.records[:len(s.records)-1]
		s.kept -= record.Size
		_ = s.root.RemoveAll(in.id)
		return Record{}, fmt.Errorf("list the shared file: %w", err)
	}
	s.notifyLocked()
	return record, nil
}

// Abort drops a share that didn't finish. Nothing of it stays.
func (in *Incoming) Abort() {
	if in.done {
		return
	}
	if in.file != nil {
		_ = in.file.Close()
		in.file = nil
	}
	_ = in.store.root.Remove(filepath.Join(incomingName, in.id))
	in.release()
}

func (in *Incoming) release() {
	if in.done {
		return
	}
	in.done = true
	in.store.mu.Lock()
	in.store.reserved -= in.size
	in.store.mu.Unlock()
}

func newID() string {
	var value [idBytes]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(fmt.Sprintf("create a shared file ID: %v", err))
	}
	return hex.EncodeToString(value[:])
}

func validID(value string) bool {
	if len(value) != idBytes*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && hex.EncodeToString(decoded) == value
}

// ValidID reports whether value has the form of a shared file ID.
func ValidID(value string) bool { return validID(value) }

// safeName is the uploads' safe name, cut to fit a file name while keeping
// its extension.
func safeName(name string) string {
	name = uploads.SafeBasename(name)
	if len(name) <= maxNameBytes {
		return name
	}
	extension := filepath.Ext(name)
	if len(extension) > 16 || strings.ContainsRune(extension, ' ') {
		extension = ""
	}
	stem := name[:len(name)-len(extension)]
	limit := maxNameBytes - len(extension)
	for limit > 0 && !utf8.RuneStart(stem[limit]) {
		limit--
	}
	return strings.TrimSpace(stem[:limit]) + extension
}
