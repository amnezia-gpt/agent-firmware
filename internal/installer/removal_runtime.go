package installer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"

	hostplatform "github.com/amnezia-gpt/agent-firmware/internal/host"
	"github.com/amnezia-gpt/agent-firmware/internal/privatefile"
)

// CheckRemovalRuntime recognizes modern enrollment codes and the socket and
// token of the legacy API. It rejects unrelated entries even inside /run.
func CheckRemovalRuntime(layout hostplatform.Layout, installation string, legacy bool) error {
	_, err := removalRuntime(layout, installation, legacy)
	return err
}

var runtimeInstallation = regexp.MustCompile(`^[a-f0-9]{32}$`)

func removalRuntime(layout hostplatform.Layout, installation string, legacy bool) ([]string, error) {
	owner := privatefile.Owner{UID: layout.UID, GID: layout.GID}
	root, err := privatefile.OpenDirectory(layout.Runtime, owner, 0700)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, ErrConflict
	}
	defer root.Close()
	entries, err := removalEntries(root, ".")
	if err != nil {
		return nil, err
	}
	var owned []string
	for _, entry := range entries {
		name := entry.Name()
		if !legacy && (name == hostplatform.RuntimeStatus || name == hostplatform.RuntimeStatus+".new") {
			info, err := root.Lstat(name)
			if err != nil || !owner.Regular(info, 0600, false) {
				return nil, ErrConflict
			}
			owned = append(owned, filepath.Join(layout.Runtime, name))
			continue
		}
		if !runtimeInstallation.MatchString(name) || legacy && installation != "" && name != installation {
			return nil, ErrConflict
		}
		files, err := removalRuntimeInstallation(root, name, legacy, owner)
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			owned = append(owned, filepath.Join(layout.Runtime, file))
		}
		owned = append(owned, layout.InstallationRuntime(name))
	}
	return append(owned, layout.Runtime), nil
}

func removalRuntimeInstallation(root *os.Root, installation string, legacy bool, owner privatefile.Owner) ([]string, error) {
	dir, err := root.Lstat(installation)
	if err != nil || !owner.Directory(dir, 0700) {
		return nil, ErrConflict
	}
	entries, err := removalEntries(root, installation)
	if err != nil {
		return nil, err
	}
	var owned []string
	for _, entry := range entries {
		name := filepath.Join(installation, entry.Name())
		info, err := root.Lstat(name)
		if err != nil {
			return nil, ErrConflict
		}
		switch entry.Name() {
		case "enrollment-code", "enrollment-code.new":
			if !owner.Regular(info, 0600, false) {
				return nil, ErrConflict
			}
		case "api", "auth":
			if !legacy {
				return nil, ErrConflict
			}
			files, err := legacyRuntimeEntries(root, name, entry.Name() == "api", info, owner)
			if err != nil {
				return nil, err
			}
			owned = append(owned, files...)
		default:
			return nil, ErrConflict
		}
		owned = append(owned, name)
	}
	return owned, nil
}

func legacyRuntimeEntries(root *os.Root, name string, api bool, info os.FileInfo, owner privatefile.Owner) ([]string, error) {
	const legacyGatewayID = 65532
	dirOwner := privatefile.Owner{UID: legacyGatewayID, GID: legacyGatewayID}
	mode, leaf := os.FileMode(0500), "token"
	if api {
		dirOwner, mode, leaf = privatefile.Owner{UID: owner.UID, GID: legacyGatewayID}, 0750, "socket"
	}
	if !dirOwner.Directory(info, mode) {
		return nil, ErrConflict
	}
	entries, err := removalEntries(root, name)
	if err != nil || len(entries) > 1 {
		return nil, ErrConflict
	}
	if len(entries) == 0 {
		return nil, nil
	}
	path := filepath.Join(name, leaf)
	file, err := root.Lstat(path)
	if err != nil || entries[0].Name() != leaf {
		return nil, ErrConflict
	}
	if api {
		actual, _, ok := privatefile.Ownership(file)
		if !ok || actual != dirOwner || file.Mode() != os.ModeSocket|0660 {
			return nil, ErrConflict
		}
	} else if !dirOwner.Regular(file, 0400, false) {
		return nil, ErrConflict
	}
	return []string{path}, nil
}

func removalEntries(root *os.Root, name string) ([]os.DirEntry, error) {
	dir, err := root.Open(name)
	if err != nil {
		return nil, ErrConflict
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return nil, ErrConflict
	}
	return entries, nil
}

func RemoveRuntime(ctx context.Context, layout hostplatform.Layout, installation string, legacy bool) error {
	paths, err := removalRuntime(layout, installation, legacy)
	if err != nil {
		return err
	}
	for _, path := range paths {
		if err := removeDurably(ctx, path); err != nil {
			return err
		}
	}
	return nil
}
