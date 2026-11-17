//go:build linux || darwin

// Package privatefile provides shared file custody and publication primitives.
package privatefile

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

var ErrUnsafe = errors.New("unsafe file")
var ErrBusy = errors.New("file lock is busy")

type Owner struct {
	UID, GID uint32
}

func EffectiveOwner() Owner { return Owner{uint32(os.Geteuid()), uint32(os.Getegid())} }

func Ownership(info os.FileInfo) (Owner, uint64, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return Owner{}, 0, false
	}
	return Owner{stat.Uid, stat.Gid}, uint64(stat.Nlink), true
}

func (o Owner) Directory(info os.FileInfo, mode os.FileMode) bool {
	owner, _, ok := Ownership(info)
	return ok && owner == o && info.IsDir() && exactMode(info, mode)
}

// Regular also admits an unlinked inode when a snapshot reader races rename.
func (o Owner) Regular(info os.FileInfo, mode os.FileMode, allowUnlinked bool) bool {
	owner, links, ok := Ownership(info)
	return ok && owner == o && info.Mode().IsRegular() && exactMode(info, mode) &&
		(links == 1 || allowUnlinked && links == 0)
}

func exactMode(info os.FileInfo, mode os.FileMode) bool {
	const special = os.ModeSetuid | os.ModeSetgid | os.ModeSticky
	return info.Mode()&(os.ModePerm|special) == mode
}

func OpenDirectory(path string, owner Owner, mode os.FileMode) (*os.Root, error) {
	// A trailing slash can make Lstat follow a directory symlink.
	path = filepath.Clean(path)
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !owner.Directory(info, mode) {
		return nil, ErrUnsafe
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, ErrUnsafe
	}
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) || !owner.Directory(opened, mode) {
		root.Close()
		return nil, ErrUnsafe
	}
	return root, nil
}

// Open uses a nonblocking, no-follow open, optionally relative to a pinned root.
func Open(root *os.Root, name string, flags int, mode os.FileMode) (*os.File, error) {
	flags |= syscall.O_NOFOLLOW | syscall.O_NONBLOCK
	if root != nil {
		return root.OpenFile(name, flags, mode)
	}
	return os.OpenFile(name, flags, mode)
}

func OpenRegular(root *os.Root, name string, flags int, mode os.FileMode, owner Owner, allowUnlinked bool) (*os.File, error) {
	file, err := Open(root, name, flags, mode)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !owner.Regular(info, mode, allowUnlinked) {
		file.Close()
		return nil, ErrUnsafe
	}
	return file, nil
}

// TryLock holds an exclusive advisory lock until the file is closed.
func TryLock(file *os.File) error {
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); errors.Is(err, syscall.EWOULDBLOCK) {
		return ErrBusy
	} else {
		return err
	}
}

// WriteNew exclusively creates and flushes a file. The installer passes
// exactPermissions to retain its explicit chmod regardless of the umask.
func WriteNew(root *os.Root, name string, data []byte, mode os.FileMode, exactPermissions bool) error {
	_, err := writeNew(root, name, data, mode, exactPermissions)
	return err
}

func writeNew(root *os.Root, name string, data []byte, mode os.FileMode, exactPermissions bool) (bool, error) {
	file, err := Open(root, name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return false, err
	}
	if !exactPermissions {
		info, err := file.Stat()
		if err != nil || !EffectiveOwner().Regular(info, mode, false) {
			file.Close()
			return true, ErrUnsafe
		}
	}
	n, err := file.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err == nil && exactPermissions {
		err = file.Chmod(mode)
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return true, err
}

// Replace publishes a complete file and flushes its directory. committed
// distinguishes a failed publication from an unacknowledged directory sync.
func Replace(root *os.Root, target, staged string, data []byte, mode os.FileMode, exactPermissions bool, beforeRename func() error, sync func(*os.File) error) (committed bool, err error) {
	rename, remove := os.Rename, os.Remove
	if root != nil {
		rename, remove = root.Rename, root.Remove
	}
	created, err := writeNew(root, staged, data, mode, exactPermissions)
	if created {
		defer func() {
			if !committed {
				_ = remove(staged)
			}
		}()
	}
	if err != nil {
		return false, err
	}
	if beforeRename != nil {
		if err := beforeRename(); err != nil {
			return false, err
		}
	}
	if err := rename(staged, target); err != nil {
		return false, err
	}
	return true, SyncDirectory(root, filepath.Dir(target), sync)
}

func SyncDirectory(root *os.Root, name string, sync func(*os.File) error) error {
	open := os.Open
	if root != nil {
		open = root.Open
	}
	dir, err := open(name)
	if err != nil {
		return err
	}
	defer dir.Close()
	if sync != nil {
		return sync(dir)
	}
	return dir.Sync()
}
