package installer

import (
	"errors"
	"os"
	"path/filepath"
	"slices"

	hostplatform "github.com/amnezia-gpt/agent-firmware/internal/host"
	"github.com/amnezia-gpt/agent-firmware/internal/privatefile"
)

// CheckParents validates directories outside the managed installation. Only
// the user layout can create its optional parent directories.
func CheckParents(layout hostplatform.Layout, create bool) error {
	owner := privatefile.Owner{UID: layout.UID, GID: layout.GID}
	for _, directory := range layout.Parents {
		info, err := os.Lstat(directory)
		if errors.Is(err, os.ErrNotExist) && slices.Contains(layout.CreateParents, directory) {
			if !create {
				continue
			}
			if err := os.Mkdir(directory, 0700); err == nil {
				// On macOS a new directory inherits its parent's group.
				if os.Lchown(directory, -1, int(layout.GID)) != nil {
					return ErrWrite
				}
			} else if !errors.Is(err, os.ErrExist) {
				return ErrWrite
			}
			if syncPath(filepath.Dir(directory)) != nil {
				return ErrWrite
			}
			info, err = os.Lstat(directory)
		}
		if err != nil {
			return ErrConflict
		}
		mode := os.FileMode(0755)
		if layout.UserService {
			mode = info.Mode().Perm()
			actual, _, ok := privatefile.Ownership(info)
			if !ok || actual.UID != owner.UID || mode&0700 != 0700 || mode&0022 != 0 {
				return ErrConflict
			}
			if (directory == filepath.Dir(layout.Base) || directory == filepath.Dir(layout.Unit)) && actual.GID != owner.GID {
				return ErrConflict
			}
			if !actual.Directory(info, mode) {
				return ErrConflict
			}
			continue
		}
		if !owner.Directory(info, mode) {
			return ErrConflict
		}
	}
	return nil
}
