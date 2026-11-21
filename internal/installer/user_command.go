package installer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	hostplatform "github.com/amnezia-gpt/agent-firmware/internal/host"
	"github.com/amnezia-gpt/agent-firmware/internal/privatefile"
)

const (
	userBin      = ".local/bin"
	commandName  = "agent-firmware"
	pathOptOut   = ".cli-path-disabled"
	maxProfile   = 1 << 20
	pathDisabled = "disabled\n"
)

const posixPath = `# agent-firmware user command
if [ -x "$HOME/.local/bin/agent-firmware" ]; then
    case ":$PATH:" in
        *":$HOME/.local/bin:"*) ;;
        *) export PATH="${PATH:+$PATH:}$HOME/.local/bin" ;;
    esac
fi
`

const fishPath = `# agent-firmware user command
if test -x "$HOME/.local/bin/agent-firmware"
    fish_add_path --append --path "$HOME/.local/bin"
end
`

func (r *run) installUserCommand() {
	l := r.Layout
	disabled := filepath.Join(filepath.Dir(l.Wrapper), pathOptOut)
	optOut := os.Getenv("AGENT_FIRMWARE_NO_MODIFY_PATH") == "1"
	if optOut {
		if err := privatefile.WriteNew(nil, disabled, []byte(pathDisabled), 0600, true); err != nil && !errors.Is(err, os.ErrExist) {
			r.say("Could not save the PATH preference. Set AGENT_FIRMWARE_NO_MODIFY_PATH=1 again on update.")
		}
	}
	bin := filepath.Join(l.Home, userBin)
	root, err := openUserDirectory(l, bin, true)
	if err != nil {
		r.say("Could not set up ~/.local/bin/agent-firmware. Use the installed command in Application Support.")
		return
	}
	defer root.Close()
	if err := root.Symlink(l.Wrapper, commandName); err != nil && !ownedCommandLink(root, l) {
		r.say("Could not set up ~/.local/bin/agent-firmware. Use the installed command in Application Support.")
		return
	}
	if found, err := exec.LookPath(commandName); err == nil {
		if !sameExecutable(found, l.Wrapper) {
			r.say("Another agent-firmware command is on PATH. Use ~/.local/bin/agent-firmware for this installation.")
		}
		return
	}
	if optOut || !errors.Is(lstatError(disabled), os.ErrNotExist) {
		r.say("Shell PATH setup is disabled. Use ~/.local/bin/agent-firmware or add ~/.local/bin to PATH.")
		return
	}
	if err := configureUserPath(l, filepath.Base(os.Getenv("SHELL"))); err != nil {
		r.say("Could not configure shell PATH. Use ~/.local/bin/agent-firmware or add ~/.local/bin to PATH.")
		return
	}
	r.say("Open a new terminal to use agent-firmware by name, or run ~/.local/bin/agent-firmware here.")
}

func sameExecutable(a, b string) bool {
	first, err := os.Stat(a)
	if err != nil {
		return false
	}
	second, err := os.Stat(b)
	return err == nil && os.SameFile(first, second)
}

func lstatError(path string) error {
	_, err := os.Lstat(path)
	return err
}

func ownedCommandLink(root *os.Root, l hostplatform.Layout) bool {
	info, err := root.Lstat(commandName)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return false
	}
	owner, _, ok := privatefile.Ownership(info)
	target, err := root.Readlink(commandName)
	return ok && owner.UID == l.UID && err == nil && target == l.Wrapper
}

func configureUserPath(l hostplatform.Layout, shell string) error {
	switch shell {
	case "zsh":
		directory := os.Getenv("ZDOTDIR")
		if directory == "" {
			directory = l.Home
		}
		return appendPathProfile(l, directory, ".zshrc")
	case "bash":
		loginProfile := ".bash_profile"
		for _, candidate := range []string{".bash_profile", ".bash_login", ".profile"} {
			if !errors.Is(lstatError(filepath.Join(l.Home, candidate)), os.ErrNotExist) {
				loginProfile = candidate
				break
			}
		}
		if err := appendPathProfile(l, l.Home, loginProfile); err != nil {
			return err
		}
		return appendPathProfile(l, l.Home, ".bashrc")
	case "fish":
		directory := os.Getenv("XDG_CONFIG_HOME")
		if directory == "" {
			directory = filepath.Join(l.Home, ".config")
		}
		root, err := openUserDirectory(l, filepath.Join(directory, "fish", "conf.d"), true)
		if err != nil {
			return err
		}
		defer root.Close()
		const fragment = "agent-firmware.fish"
		err = privatefile.WriteNew(root, fragment, []byte(fishPath), 0600, true)
		if errors.Is(err, os.ErrExist) && userFileHasContents(root, fragment, l.UID, fishPath) {
			return nil
		}
		return err
	default:
		return ErrConflict
	}
}

func appendPathProfile(l hostplatform.Layout, directory, name string) error {
	root, err := openUserDirectory(l, directory, true)
	if err != nil {
		return err
	}
	defer root.Close()
	f, err := privatefile.Open(root, name, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if !safeUserFile(f, l.UID) {
		return ErrConflict
	}
	data, err := io.ReadAll(io.LimitReader(f, maxProfile+1))
	if err != nil || len(data) > maxProfile {
		return ErrConflict
	}
	if bytes.Contains(data, []byte(posixPath)) {
		return nil
	}
	if _, err := io.WriteString(f, "\n"+posixPath); err != nil {
		return err
	}
	return f.Sync()
}

func safeUserFile(f *os.File, uid uint32) bool {
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 {
		return false
	}
	actual, links, ok := privatefile.Ownership(info)
	return ok && actual.UID == uid && links == 1
}

func userFileHasContents(root *os.Root, name string, uid uint32, expected string) bool {
	f, err := privatefile.Open(root, name, os.O_RDONLY, 0)
	if err != nil {
		return false
	}
	defer f.Close()
	if !safeUserFile(f, uid) {
		return false
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(len(expected)+1)))
	return err == nil && string(data) == expected
}

// Check user-owned directories from home to the target before creating or
// opening files. Keep automatic PATH setup inside the account's home.
func openUserDirectory(l hostplatform.Layout, directory string, create bool) (*os.Root, error) {
	directory = filepath.Clean(directory)
	rel, err := filepath.Rel(l.Home, directory)
	if err != nil || !filepath.IsAbs(l.Home) || !filepath.IsLocal(rel) {
		return nil, ErrConflict
	}
	path := l.Home
	parts := strings.Split(rel, string(filepath.Separator))
	for _, part := range append([]string{"."}, parts...) {
		path = filepath.Join(path, part)
		info, err := os.Lstat(path)
		if create && path != l.Home && errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(path, 0700); err != nil && !errors.Is(err, os.ErrExist) {
				return nil, err
			}
			info, err = os.Lstat(path)
		}
		if err != nil {
			return nil, err
		}
		owner, _, ok := privatefile.Ownership(info)
		mode := info.Mode().Perm()
		if !ok || owner.UID != l.UID || !owner.Directory(info, mode) || mode&0700 != 0700 || mode&0022 != 0 {
			return nil, ErrConflict
		}
		if path == directory {
			return privatefile.OpenDirectory(path, owner, mode)
		}
	}
	return nil, ErrConflict
}

func (r *RemovalHost) removeUserCommand(ctx context.Context) error {
	if ctx.Err() != nil {
		return ErrInterrupted
	}
	l := r.Layout
	if root, err := openUserDirectory(l, filepath.Join(l.Home, userBin), false); err == nil {
		defer root.Close()
		if ownedCommandLink(root, l) {
			if err := root.Remove(commandName); err != nil && !errors.Is(err, os.ErrNotExist) {
				return ErrWrite
			}
		}
	}
	directory := filepath.Dir(l.Wrapper)
	if root, err := openUserDirectory(l, directory, false); err == nil {
		defer root.Close()
		if userFileHasContents(root, pathOptOut, l.UID, pathDisabled) {
			if err := root.Remove(pathOptOut); err != nil && !errors.Is(err, os.ErrNotExist) {
				return ErrWrite
			}
		}
	}
	return nil
}
