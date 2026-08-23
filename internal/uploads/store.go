package uploads

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"syscall"
)

const (
	associationStateVersion = 1
	maxAssociationState     = 4 << 20
)

type persistedState struct {
	Associations []association `json:"associations"`
	Version      int           `json:"version"`
}

func readAssociations(path string) (map[string]association, error) {
	if path == "" {
		return nil, errors.New("upload association state path is required")
	}
	file, err := openPrivateRegular(path, "upload association state")
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxAssociationState+1))
	if err != nil || len(data) > maxAssociationState {
		return nil, errors.New("upload association state cannot be read within its size bound")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var state persistedState
	if err := decoder.Decode(&state); err != nil {
		return nil, fmt.Errorf("decode upload association state: %w", err)
	}
	if err := requireJSONEnd(decoder); err != nil {
		return nil, err
	}
	if state.Version != associationStateVersion {
		return nil, fmt.Errorf("upload association state version %d is not supported", state.Version)
	}
	records := make(map[string]association, len(state.Associations))
	for _, record := range state.Associations {
		if !validIdentity(record.WorkspaceID) || records[record.WorkspaceID].WorkspaceID != "" {
			return nil, errors.New("upload association state has an invalid or duplicate workspace")
		}
		if !filepath.IsAbs(record.Directory) || filepath.Clean(record.Directory) != record.Directory {
			return nil, errors.New("upload association state has a non-absolute directory")
		}
		records[record.WorkspaceID] = record
	}
	return records, nil
}

func writeAssociations(path string, records map[string]association) error {
	state := persistedState{Associations: make([]association, 0, len(records)), Version: associationStateVersion}
	for _, record := range records {
		state.Associations = append(state.Associations, record)
	}
	sort.Slice(state.Associations, func(i, j int) bool { return state.Associations[i].WorkspaceID < state.Associations[j].WorkspaceID })
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode upload association state: %w", err)
	}
	data = append(data, '\n')
	if len(data) > maxAssociationState {
		return errors.New("upload association state is too large")
	}
	directory := filepath.Dir(path)
	if err := ensurePrivateDirectory(directory); err != nil {
		return err
	}
	if _, err := os.Lstat(path); err == nil {
		file, err := openPrivateRegular(path, "upload association state")
		if err != nil {
			return err
		}
		_ = file.Close()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".uploads-*")
	if err != nil {
		return fmt.Errorf("create upload association state: %w", err)
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("write upload association state: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync upload association state: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("replace upload association state: %w", err)
	}
	directoryFile, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer directoryFile.Close()
	return directoryFile.Sync()
}

func ensurePrivateDirectory(directory string) error {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create upload state directory: %w", err)
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 || !ownedByCurrentUser(info) {
		return errors.New("upload state directory must be an owner-only 0700 directory")
	}
	return nil
}

func openPrivateRegular(path, description string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !ownedByCurrentUser(info) || linkCount(info) != 1 {
		file.Close()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("%s must be one owner-only 0600 regular file", description)
	}
	return file, nil
}

func ownedByCurrentUser(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}

func linkCount(info os.FileInfo) uint64 {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0
	}
	return uint64(stat.Nlink)
}

func requireJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("upload state contains extra JSON")
	}
	return nil
}
