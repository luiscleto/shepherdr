package herdr

import (
	"os"
	"path/filepath"
	"sync"
)

// repositoryFolders remembers the repository folder of each folder a shown
// core space uses, so the Home projection never touches files. A folder seen
// for the first time suggests nothing until its check, run once in the
// background, finishes and asks Home to publish again. A stalled mount holds
// only its own check. Folders no shown space uses are forgotten, so one that
// comes back is checked again.
type repositoryFolders struct {
	check    func(folder string) string
	resolved chan struct{}
	// used is what the projection asked for since the last settle. Only the
	// projector's goroutine touches it.
	used map[string]struct{}

	mu       sync.Mutex
	known    map[string]string
	checking map[string]struct{}
	wanted   map[string]struct{}
}

func newRepositoryFolders(check func(folder string) string) *repositoryFolders {
	return &repositoryFolders{
		check: check, resolved: make(chan struct{}, 1), used: map[string]struct{}{},
		known: map[string]string{}, checking: map[string]struct{}{}, wanted: map[string]struct{}{},
	}
}

// lookup is the projection's read: the folder's repository folder, or "" when
// it has none or isn't checked yet.
func (r *repositoryFolders) lookup(folder string) string {
	r.used[folder] = struct{}{}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.known[folder]
}

// settle follows a projection: it forgets the folders that projection didn't
// use and starts one check for each new one, unless one is still running.
func (r *repositoryFolders) settle() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.wanted, r.used = r.used, map[string]struct{}{}
	for folder := range r.known {
		if _, wanted := r.wanted[folder]; !wanted {
			delete(r.known, folder)
		}
	}
	for folder := range r.wanted {
		_, known := r.known[folder]
		_, checking := r.checking[folder]
		if known || checking {
			continue
		}
		r.checking[folder] = struct{}{}
		go func() {
			repository := r.check(folder)
			r.mu.Lock()
			defer r.mu.Unlock()
			delete(r.checking, folder)
			if _, wanted := r.wanted[folder]; wanted {
				r.known[folder] = repository
				latchDirty(r.resolved)
			}
		}()
	}
}

// repositoryFolder finds the Git repository that folder is in by looking at
// files only: symlinks resolved, then the nearest folder up from it with a
// .git entry. Only a top-level repository counts, as on the Herdr TUI: the
// entry must be a real folder holding HEAD. Any other .git entry, such as the
// file a linked worktree or submodule has, or a symlink, ends the search with
// "". It is also "" for a folder outside a repository or one that doesn't
// exist.
func repositoryFolder(folder string) string {
	if !filepath.IsAbs(folder) {
		return ""
	}
	current, err := filepath.EvalSymlinks(folder)
	if err != nil {
		return ""
	}
	for {
		git := filepath.Join(current, ".git")
		if info, err := os.Lstat(git); err == nil {
			if !info.IsDir() {
				return ""
			}
			if _, err := os.Stat(filepath.Join(git, "HEAD")); err != nil {
				return ""
			}
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		current = parent
	}
}
