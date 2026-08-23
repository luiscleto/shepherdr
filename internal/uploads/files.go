package uploads

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

type marker struct {
	Ownership   string `json:"ownership"`
	WorkspaceID string `json:"workspace_id"`
}

type ownedDirectory struct {
	name       string
	ownsParent bool
	parent     *os.Root
	record     association
	root       *os.Root
}

type stagedFile struct {
	name string
	path string
}

func writeMarker(directory *ownedDirectory) error {
	data, err := json.Marshal(marker{Ownership: directory.record.Ownership, WorkspaceID: directory.record.WorkspaceID})
	if err != nil {
		return err
	}
	data = append(data, '\n')
	file, err := openExclusiveAt(directory.root, markerName, 0o600)
	if err != nil {
		return fmt.Errorf("create upload marker: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return fmt.Errorf("write upload marker: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("sync upload marker: %w", err)
	}
	if err := file.Close(); err != nil {
		return err
	}
	directoryFile, err := directory.root.Open(".")
	if err != nil {
		return fmt.Errorf("open upload directory for marker sync: %w", err)
	}
	defer directoryFile.Close()
	return directoryFile.Sync()
}

func validateAssociationRecord(record association) error {
	if !validIdentity(record.WorkspaceID) || len(record.Ownership) != 32 {
		return errors.New("upload association fields are invalid")
	}
	if _, err := hexOwnership(record.Ownership); err != nil {
		return err
	}
	if !filepath.IsAbs(record.Directory) || filepath.Clean(record.Directory) != record.Directory ||
		!strings.HasSuffix(filepath.Base(record.Directory), "-"+record.Ownership) {
		return errors.New("upload association path does not match its ownership")
	}
	return nil
}

func openCreatedDirectory(parent *os.Root, rootPath, name string, record association) (*ownedDirectory, error) {
	entry, err := parent.Lstat(name)
	if err != nil {
		return nil, err
	}
	root, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	directory := &ownedDirectory{name: name, parent: parent, record: record, root: root}
	if record.Directory != filepath.Join(rootPath, name) {
		closeOwnedDirectory(directory)
		return nil, errors.New("created upload directory path changed")
	}
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(entry, opened) || !ownerOnlyDirectory(entry) {
		closeOwnedDirectory(directory)
		return nil, errors.New("created upload directory is not the owner-only directory that was opened")
	}
	return directory, nil
}

func openOwnedDirectory(record association) (*ownedDirectory, bool, error) {
	if err := validateAssociationRecord(record); err != nil {
		return nil, false, err
	}
	parent, err := os.OpenRoot(filepath.Dir(record.Directory))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	name := filepath.Base(record.Directory)
	entry, err := parent.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		parent.Close()
		return nil, false, nil
	}
	if err != nil {
		parent.Close()
		return nil, false, err
	}
	root, err := parent.OpenRoot(name)
	if err != nil {
		parent.Close()
		return nil, false, err
	}
	directory := &ownedDirectory{name: name, ownsParent: true, parent: parent, record: record, root: root}
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(entry, opened) {
		closeOwnedDirectory(directory)
		return nil, false, errors.New("upload directory changed while it was opened")
	}
	present, err := verifyOwnedDirectory(directory)
	if err != nil || !present {
		closeOwnedDirectory(directory)
		return nil, present, err
	}
	return directory, true, nil
}

func verifyOwnedDirectory(directory *ownedDirectory) (bool, error) {
	if directory == nil || directory.root == nil || directory.parent == nil {
		return false, errors.New("upload directory handle is unavailable")
	}
	if err := validateAssociationRecord(directory.record); err != nil {
		return false, err
	}
	entry, err := directory.parent.Lstat(directory.name)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	opened, err := directory.root.Stat(".")
	if err != nil {
		return false, err
	}
	if !os.SameFile(entry, opened) {
		return false, errors.New("upload directory no longer matches its open handle")
	}
	if !ownerOnlyDirectory(opened) {
		return false, errors.New("upload directory is not an owner-only 0700 directory")
	}
	file, err := openPrivateRegularAt(directory.root, markerName, "upload marker")
	if err != nil {
		return false, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(data) > 4096 {
		return false, errors.New("upload marker cannot be read within its size bound")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var value marker
	if err := decoder.Decode(&value); err != nil {
		return false, errors.New("upload marker is invalid")
	}
	if err := requireJSONEnd(decoder); err != nil {
		return false, err
	}
	if value.WorkspaceID != directory.record.WorkspaceID || value.Ownership != directory.record.Ownership {
		return false, errors.New("upload marker does not match its association")
	}
	return true, nil
}

func verifyPathBinding(directory *ownedDirectory) error {
	bound, present, err := openOwnedDirectory(directory.record)
	if err != nil {
		return err
	}
	if !present {
		return errors.New("upload directory path no longer reaches its verified directory")
	}
	defer closeOwnedDirectory(bound)
	opened, err := directory.root.Stat(".")
	if err != nil {
		return err
	}
	current, err := bound.root.Stat(".")
	if err != nil {
		return err
	}
	if !os.SameFile(opened, current) {
		return errors.New("upload directory path was redirected")
	}
	return nil
}

func ownerOnlyDirectory(info os.FileInfo) bool {
	return info.IsDir() && info.Mode()&os.ModeSymlink == 0 && info.Mode().Perm() == 0o700 && ownedByCurrentUser(info)
}

func openPrivateRegularAt(root *os.Root, name, description string) (*os.File, error) {
	entry, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	file, err := root.OpenFile(name, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(entry, opened) || !opened.Mode().IsRegular() || opened.Mode().Perm() != 0o600 ||
		!ownedByCurrentUser(opened) || linkCount(opened) != 1 {
		file.Close()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("%s must be one owner-only 0600 regular file", description)
	}
	return file, nil
}

func hexOwnership(value string) ([]byte, error) {
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != 16 || hex.EncodeToString(decoded) != value {
		return nil, errors.New("upload ownership value is invalid")
	}
	return decoded, nil
}

func stageFiles(ctx context.Context, directory *ownedDirectory, files []File) ([]stagedFile, error) {
	present, err := verifyOwnedDirectory(directory)
	if err != nil || !present {
		if err == nil {
			err = errors.New("upload directory disappeared")
		}
		return nil, err
	}
	staged := make([]stagedFile, 0, len(files))
	for _, upload := range files {
		if err := ctx.Err(); err != nil {
			return nil, rollbackStageFiles(directory.root, staged, err)
		}
		name := safeBasename(upload.Name)
		created, file, err := reserveFile(directory, name)
		if err != nil {
			return nil, rollbackStageFiles(directory.root, staged, err)
		}
		staged = append(staged, created)
		if err := writeUploadedFile(ctx, file, upload.Data); err != nil {
			file.Close()
			return nil, rollbackStageFiles(directory.root, staged, fmt.Errorf("write uploaded file: %w", err))
		}
		if err := file.Close(); err != nil {
			return nil, rollbackStageFiles(directory.root, staged, fmt.Errorf("close uploaded file: %w", err))
		}
	}
	return staged, nil
}

func rollbackStageFiles(root *os.Root, files []stagedFile, cause error) error {
	if err := removeRequestFiles(root, files); err != nil {
		return errors.Join(cause, fmt.Errorf("uploaded file rollback was incomplete: %w", err))
	}
	return cause
}

func writeUploadedFile(ctx context.Context, file *os.File, data []byte) error {
	const chunkSize = 64 << 10
	for len(data) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		length := min(len(data), chunkSize)
		written, err := file.Write(data[:length])
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		data = data[written:]
	}
	return ctx.Err()
}

func safeBasename(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = name[strings.LastIndex(name, "/")+1:]
	var builder strings.Builder
	for _, character := range name {
		if character == '/' || character == '\\' || unsafePathRune(character) {
			continue
		}
		builder.WriteRune(character)
	}
	name = strings.TrimSpace(builder.String())
	if name == "" || name == "." || name == ".." || name == markerName {
		return "file"
	}
	return name
}

func reserveFile(directory *ownedDirectory, name string) (stagedFile, *os.File, error) {
	stem, extension := splitExtension(name)
	for index := 0; ; index++ {
		candidate := name
		if index > 0 {
			candidate = fmt.Sprintf("%s (%d)%s", stem, index, extension)
		}
		file, err := openExclusiveAt(directory.root, candidate, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return stagedFile{}, nil, fmt.Errorf("reserve uploaded filename: %w", err)
		}
		return stagedFile{name: candidate, path: filepath.Join(directory.record.Directory, candidate)}, file, nil
	}
}

func splitExtension(name string) (string, string) {
	index := strings.LastIndexByte(name, '.')
	if index <= 0 || index == len(name)-1 {
		return name, ""
	}
	return name[:index], name[index:]
}

func openExclusiveAt(root *os.Root, name string, mode os.FileMode) (*os.File, error) {
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != mode || !ownedByCurrentUser(info) || linkCount(info) != 1 {
		file.Close()
		if err != nil {
			return nil, err
		}
		return nil, errors.New("created upload file is not one owner-only regular file")
	}
	return file, nil
}

func removeRequestFiles(root *os.Root, files []stagedFile) error {
	var combined error
	for _, file := range files {
		if err := root.Remove(file.name); err != nil && !errors.Is(err, os.ErrNotExist) {
			combined = errors.Join(combined, err)
		}
	}
	return combined
}

func removeOwnedDirectory(directory *ownedDirectory, requireMarker bool) error {
	if requireMarker {
		present, err := verifyOwnedDirectory(directory)
		if err != nil {
			return err
		}
		if !present {
			return errors.New("owned upload directory disappeared")
		}
	} else {
		entry, err := directory.parent.Lstat(directory.name)
		if err != nil {
			return err
		}
		opened, err := directory.root.Stat(".")
		if err != nil || !os.SameFile(entry, opened) || !ownerOnlyDirectory(opened) {
			return errors.New("created upload directory no longer matches its open handle")
		}
	}
	handle, err := directory.root.Open(".")
	if err != nil {
		return err
	}
	entries, err := handle.ReadDir(-1)
	_ = handle.Close()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := directory.root.RemoveAll(entry.Name()); err != nil {
			return err
		}
	}
	entry, err := directory.parent.Lstat(directory.name)
	if err != nil {
		return err
	}
	opened, err := directory.root.Stat(".")
	if err != nil || !os.SameFile(entry, opened) {
		return errors.New("owned upload directory changed before removal")
	}
	return directory.parent.Remove(directory.name)
}

func closeOwnedDirectory(directory *ownedDirectory) {
	if directory == nil {
		return
	}
	if directory.root != nil {
		_ = directory.root.Close()
		directory.root = nil
	}
	if directory.ownsParent && directory.parent != nil {
		_ = directory.parent.Close()
	}
	directory.parent = nil
}

func unsafePathRune(character rune) bool {
	if unicode.IsControl(character) || character == '\u2028' || character == '\u2029' {
		return true
	}
	if character == '\u200c' || character == '\u200d' {
		return false
	}
	return unicode.Is(unicode.Cf, character)
}

func ValidatePathText(value string) error {
	if !utf8.ValidString(value) {
		return errors.New("path is not valid UTF-8")
	}
	for _, character := range value {
		if unsafePathRune(character) {
			return errors.New("path contains an unsafe terminal character")
		}
	}
	if strings.Contains(value, "\x1b[200~") || strings.Contains(value, "\x1b[201~") {
		return errors.New("path contains a bracketed-paste delimiter")
	}
	return nil
}
