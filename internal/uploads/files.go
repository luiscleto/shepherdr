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
	"syscall"
	"unicode"
	"unicode/utf8"
)

type marker struct {
	Ownership   string `json:"ownership"`
	WorkspaceID string `json:"workspace_id"`
}

func writeMarker(record association) error {
	data, err := json.Marshal(marker{Ownership: record.Ownership, WorkspaceID: record.WorkspaceID})
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := filepath.Join(record.Directory, markerName)
	file, err := openExclusive(path, 0o600)
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
	directory, err := os.Open(record.Directory)
	if err != nil {
		return fmt.Errorf("open upload directory for marker sync: %w", err)
	}
	defer directory.Close()
	return directory.Sync()
}

func verifyAssociation(record association) (bool, error) {
	if !validIdentity(record.WorkspaceID) || len(record.Ownership) != 32 {
		return false, errors.New("upload association fields are invalid")
	}
	if _, err := hexOwnership(record.Ownership); err != nil {
		return false, err
	}
	if !filepath.IsAbs(record.Directory) || filepath.Clean(record.Directory) != record.Directory ||
		!strings.HasSuffix(filepath.Base(record.Directory), "-"+record.Ownership) {
		return false, errors.New("upload association path does not match its ownership")
	}
	info, err := os.Lstat(record.Directory)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 || !ownedByCurrentUser(info) {
		return false, errors.New("upload directory is not an owner-only 0700 directory")
	}
	file, err := openPrivateRegular(filepath.Join(record.Directory, markerName), "upload marker")
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
	if value.WorkspaceID != record.WorkspaceID || value.Ownership != record.Ownership {
		return false, errors.New("upload marker does not match its association")
	}
	return true, nil
}

func hexOwnership(value string) ([]byte, error) {
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != 16 || hex.EncodeToString(decoded) != value {
		return nil, errors.New("upload ownership value is invalid")
	}
	return decoded, nil
}

func stageFiles(ctx context.Context, record association, files []File) ([]string, error) {
	present, err := verifyAssociation(record)
	if err != nil || !present {
		if err == nil {
			err = errors.New("upload directory disappeared")
		}
		return nil, err
	}
	paths := make([]string, 0, len(files))
	for _, upload := range files {
		if err := ctx.Err(); err != nil {
			return nil, rollbackStageFiles(paths, err)
		}
		name := safeBasename(upload.Name)
		path, file, err := reserveFile(record.Directory, name)
		if err != nil {
			return nil, rollbackStageFiles(paths, err)
		}
		paths = append(paths, path)
		if err := writeUploadedFile(ctx, file, upload.Data); err != nil {
			file.Close()
			return nil, rollbackStageFiles(paths, fmt.Errorf("write uploaded file: %w", err))
		}
		if err := file.Close(); err != nil {
			return nil, rollbackStageFiles(paths, fmt.Errorf("close uploaded file: %w", err))
		}
	}
	return paths, nil
}

func rollbackStageFiles(paths []string, cause error) error {
	if err := removeRequestFiles(paths); err != nil {
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
		if character == '/' || character == '\\' || unicode.IsControl(character) {
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

func reserveFile(directory, name string) (string, *os.File, error) {
	stem, extension := splitExtension(name)
	for index := 0; ; index++ {
		candidate := name
		if index > 0 {
			candidate = fmt.Sprintf("%s (%d)%s", stem, index, extension)
		}
		path := filepath.Join(directory, candidate)
		file, err := openExclusive(path, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", nil, fmt.Errorf("reserve uploaded filename: %w", err)
		}
		return path, file, nil
	}
}

func splitExtension(name string) (string, string) {
	index := strings.LastIndexByte(name, '.')
	if index <= 0 || index == len(name)-1 {
		return name, ""
	}
	return name[:index], name[index:]
}

func openExclusive(path string, mode os.FileMode) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, uint32(mode))
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
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

func removeRequestFiles(paths []string) error {
	var combined error
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			combined = errors.Join(combined, err)
		}
	}
	return combined
}

func ValidatePathText(value string) error {
	if !utf8.ValidString(value) {
		return errors.New("path is not valid UTF-8")
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return errors.New("path contains a terminal control character")
		}
	}
	if strings.Contains(value, "\x1b[200~") || strings.Contains(value, "\x1b[201~") {
		return errors.New("path contains a bracketed-paste delimiter")
	}
	return nil
}
