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

var installedDigest = regexp.MustCompile(`^[a-f0-9]{64}$`)

// RemovalHost is an inventory of owned service and binary artifacts. The
// lifecycle lock must remain held from inspection through removal.
type RemovalHost struct {
	run
	unit, loaded bool
	paths        []string
}

func InspectRemovalHost(ctx context.Context, layout hostplatform.Layout, services ServiceManager, resuming bool) (*RemovalHost, error) {
	r := &RemovalHost{run: run{Request: Request{Layout: layout, Services: services}}}
	if err := CheckParents(layout, false); err != nil {
		return nil, err
	}
	for _, path := range layout.DropIns {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return nil, ErrConflict
		}
	}
	if _, err := os.Lstat(layout.Unit + ".install-new"); !errors.Is(err, os.ErrNotExist) {
		return nil, ErrRecovery
	}
	fragment, err := services.Fragment(ctx)
	if err != nil {
		return nil, r.serviceError()
	}
	if fragment != "" && fragment != layout.Unit {
		return nil, ErrConflict
	}
	r.loaded = fragment != ""
	if _, err := os.Lstat(layout.Unit); err == nil {
		if !r.safeFile(layout.Unit, 0644) || !checkDefinition(layout.Unit, services.Owns) {
			return nil, ErrConflict
		}
		r.unit = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, ErrConflict
	} else if r.loaded && (!resuming || services.Active(ctx)) {
		return nil, ErrConflict
	}
	if err := r.inspectVersions(resuming); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(layout.Wrapper); err == nil {
		owner, _, ok := privatefile.Ownership(info)
		if !ok || owner != r.owner() || !r.ownWrapper() || !isLink(filepath.Join(layout.Base, "current")) {
			return nil, ErrConflict
		}
		r.paths = append([]string{layout.Wrapper}, r.paths...)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, ErrConflict
	}
	return r, nil
}

func (r *RemovalHost) inspectVersions(resuming bool) error {
	base := r.Layout.Base
	if _, err := os.Lstat(base); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if !r.safeDir(base, 0755) {
		return ErrConflict
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		return ErrConflict
	}
	if resuming && len(entries) == 0 {
		r.paths = append(r.paths, base)
		return nil
	}
	marker := filepath.Join(base, ".managed")
	if !r.safeFile(marker, 0644) || !hasLine(marker, managedMarker) {
		return ErrConflict
	}
	for _, entry := range entries {
		path := filepath.Join(base, entry.Name())
		switch entry.Name() {
		case ".managed", "releases":
		case "current", "current.install-new":
			target, err := os.Readlink(path)
			if err != nil || !r.validPointer(target) {
				return ErrConflict
			}
			r.paths = append(r.paths, path)
		case ".transaction":
			return ErrRecovery
		default:
			return ErrConflict
		}
	}
	releases := filepath.Join(base, "releases")
	if _, err := os.Lstat(releases); !errors.Is(err, os.ErrNotExist) {
		if !r.safeDir(releases, 0755) {
			return ErrConflict
		}
		versions, err := os.ReadDir(releases)
		if err != nil {
			return ErrConflict
		}
		for _, version := range versions {
			dir := filepath.Join(releases, version.Name())
			if !installedDigest.MatchString(version.Name()) || !r.safeDir(dir, 0755) {
				return ErrConflict
			}
			files, err := os.ReadDir(dir)
			if err != nil {
				return ErrConflict
			}
			if !resuming || len(files) != 0 {
				if len(files) != 1 || files[0].Name() != "agent-firmware" ||
					!r.validPointer("releases/"+version.Name()) {
					return ErrConflict
				}
				r.paths = append(r.paths, filepath.Join(dir, "agent-firmware"))
			}
			r.paths = append(r.paths, dir)
		}
		r.paths = append(r.paths, releases)
	}
	r.paths = append(r.paths, marker, base)
	return nil
}

func (r *RemovalHost) Present() bool { return r.unit || r.loaded || len(r.paths) > 0 }

func (r *RemovalHost) Stop(ctx context.Context) error {
	if !r.unit {
		return nil
	}
	if r.Services.Stop(ctx) != nil {
		return ErrStop
	}
	if r.Services.SetEnabled(ctx, false) != nil {
		return ErrEnable
	}
	return nil
}

func (r *RemovalHost) Remove(ctx context.Context) error {
	for _, path := range r.paths {
		if err := removeDurably(ctx, path); err != nil {
			return err
		}
	}
	if r.unit {
		if err := removeDurably(ctx, r.Layout.Unit); err != nil {
			return err
		}
	}
	if (r.unit || r.loaded) && r.Services.Reload(ctx) != nil {
		return ErrReload
	}
	return nil
}

func removeDurably(ctx context.Context, path string) error {
	if ctx.Err() != nil {
		return ErrInterrupted
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return ErrWrite
	}
	if syncPath(filepath.Dir(path)) != nil {
		return ErrWrite
	}
	return nil
}
