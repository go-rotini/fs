package fs

const opChown = "chown"

// ChownRecursive walks root and applies uid/gid to every entry,
// including root itself. Passing -1 for uid or gid leaves that value
// unchanged on POSIX (matching [os.Chown] semantics).
//
// On Windows, and other platforms without POSIX ownership, it returns
// [ErrNotSupported]; POSIX uid/gid does not map cleanly onto NTFS
// ACLs.
//
// Symlinks are not followed; the link itself has its ownership
// changed, so link targets are not affected. The walk goes through
// open directory descriptors (openat and fchownat with
// AT_SYMLINK_NOFOLLOW), so swapping a directory in the tree for a
// symlink while it runs cannot redirect it outside root. The walk
// stops at the first error.
//
// A hard link is the file it names, so a hard link in the tree to a
// file outside root changes that file's owner too. Anyone who can
// write in the tree can add such a link (Linux refuses links to files
// the caller doesn't own while fs.protected_hardlinks is set; macOS
// and the BSDs have no such guard), so don't run ChownRecursive with
// privileges over a tree that other users can write to.
func ChownRecursive(root string, uid, gid int) error {
	return chownRecursive(root, uid, gid)
}
