package uploads

import "os"

// The shared files store keeps its own copies with the same owner-only and
// safe-name rules as uploads, so it uses these instead of its own.

// SafeBasename is a file name's last part with path, control, format and
// shell characters removed, or "file".
func SafeBasename(name string) string { return safeBasename(name) }

// OpenPrivateRegular opens path only if it is one owner-only 0600 regular
// file, not a link.
func OpenPrivateRegular(path, description string) (*os.File, error) {
	return openPrivateRegular(path, description)
}

// OpenPrivateRegularAt opens name in root only if it is one owner-only 0600
// regular file, not a link.
func OpenPrivateRegularAt(root *os.Root, name, description string) (*os.File, error) {
	return openPrivateRegularAt(root, name, description)
}

// OwnerOnlyDirectory reports a 0700 directory, not a link, owned by the
// current user.
func OwnerOnlyDirectory(info os.FileInfo) bool { return ownerOnlyDirectory(info) }

// OwnedByCurrentUser reports whether the current user owns info's file.
func OwnedByCurrentUser(info os.FileInfo) bool { return ownedByCurrentUser(info) }

// LinkCount is info's hard link count, or 0 when unknown.
func LinkCount(info os.FileInfo) uint64 { return linkCount(info) }
