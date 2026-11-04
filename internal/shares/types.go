// Package shares keeps the files agents share with `shepherdr share`: an
// owner-only folder with one subfolder per file, an index beside them, and the
// local socket the command streams to.
package shares

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"strings"
	"time"
)

// Record is one shared file. The index keeps these, newest last.
type Record struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	SharedAt time.Time `json:"shared_at"`
	// Seen is set once a Shared files list that includes the file was in view
	// on any device.
	Seen bool `json:"seen"`
	// Source is the checked terminal the file came from; nil is Outside Herdr.
	Source *Source `json:"source,omitempty"`
}

// Source is the exact terminal a file came from, checked at share time. The
// process and its start time make the key: Herdr may give a later terminal
// the same pane or terminal ID, but never the same running process.
type Source struct {
	Herdr       string `json:"herdr"`
	WorkspaceID string `json:"workspace_id"`
	PaneID      string `json:"pane_id"`
	TerminalID  string `json:"terminal_id"`
	PID         int    `json:"pid"`
	Start       uint64 `json:"start"`
	// Display labels copied at share time.
	Place string `json:"place"`
	Tab   string `json:"tab,omitempty"`
	Agent string `json:"agent,omitempty"`
}

// Key is the exact terminal: no other terminal, earlier or later, has it.
func (s Source) Key() string {
	return fmt.Sprintf("%s|%s|%s|%d|%d", s.Herdr, s.PaneID, s.TerminalID, s.PID, s.Start)
}

// Handle is an opaque form of Key for the browser, which only compares it.
func (s Source) Handle() string {
	sum := sha256.Sum256([]byte(s.Key()))
	return hex.EncodeToString(sum[:12])
}

// Kind is how the browser may show a file: image, video, text, pdf or other.
type Kind string

const (
	KindImage Kind = "image"
	KindVideo Kind = "video"
	KindText  Kind = "text"
	KindPDF   Kind = "pdf"
	KindOther Kind = "other"
)

type fileType struct {
	contentType string
	kind        Kind
}

// fileTypes is the fixed preview allowlist, by extension. Everything else,
// HTML included, is served as application/octet-stream for download only.
// Content is never sniffed.
var fileTypes = map[string]fileType{
	"png":  {"image/png", KindImage},
	"jpg":  {"image/jpeg", KindImage},
	"jpeg": {"image/jpeg", KindImage},
	"gif":  {"image/gif", KindImage},
	"webp": {"image/webp", KindImage},
	"avif": {"image/avif", KindImage},
	"svg":  {"image/svg+xml", KindImage},
	"mp4":  {"video/mp4", KindVideo},
	"m4v":  {"video/mp4", KindVideo},
	"webm": {"video/webm", KindVideo},
	"mov":  {"video/quicktime", KindVideo},
	"pdf":  {"application/pdf", KindPDF},
}

var textExtensions = map[string]bool{
	"txt": true, "md": true, "markdown": true, "log": true, "json": true, "jsonl": true, "csv": true, "tsv": true,
	"yaml": true, "yml": true, "toml": true, "ini": true, "diff": true, "patch": true,
}

func extension(name string) string {
	return strings.ToLower(strings.TrimPrefix(path.Ext(name), "."))
}

// TypeOf is a file name's served Content-Type and how it may be shown.
func TypeOf(name string) (contentType string, kind Kind) {
	ext := extension(name)
	if known, ok := fileTypes[ext]; ok {
		return known.contentType, known.kind
	}
	if textExtensions[ext] {
		return "text/plain; charset=utf-8", KindText
	}
	return "application/octet-stream", KindOther
}

// Tag is the short type label shown in place of a thumbnail, such as PDF.
func Tag(name string) string {
	ext := strings.ToUpper(extension(name))
	if ext == "JPEG" {
		return "JPG"
	}
	if ext == "" || len(ext) > 4 || strings.IndexFunc(ext, func(r rune) bool { return (r < 'A' || r > 'Z') && (r < '0' || r > '9') }) >= 0 {
		return "FILE"
	}
	return ext
}

// FormatSize is a size as the Shared files page shows it, such as 96 KB.
func FormatSize(size int64) string {
	switch {
	case size < 1000:
		return fmt.Sprintf("%d B", size)
	case size < 1000*1000:
		return trimSize(float64(size)/1000) + " KB"
	case size < 1000*1000*1000:
		return trimSize(float64(size)/(1000*1000)) + " MB"
	default:
		return trimSize(float64(size)/(1000*1000*1000)) + " GB"
	}
}

func trimSize(value float64) string {
	if value >= 10 {
		return fmt.Sprintf("%.0f", value)
	}
	return strings.TrimSuffix(fmt.Sprintf("%.1f", value), ".0")
}
