package installer

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func userCommandHost(t *testing.T, shell string) *host {
	t.Helper()
	h := newHost(t)
	h.layout.UserService = true
	h.layout.Home = filepath.Dir(h.layout.State)
	h.writeEngineAddress = true
	t.Setenv("SHELL", shell)
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("ZDOTDIR", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("AGENT_FIRMWARE_NO_MODIFY_PATH", "")
	return h
}

func TestUserCommandFollowsInstallUpgradeAndRemoval(t *testing.T) {
	for _, test := range []struct {
		shell   string
		profile string
	}{
		{"/bin/zsh", ".zshrc"},
		{"/bin/bash", ".bash_profile"},
		{"/bin/fish", ".config/fish/conf.d/agent-firmware.fish"},
	} {
		t.Run(filepath.Base(test.shell), func(t *testing.T) {
			shell, _ := exec.LookPath(filepath.Base(test.shell))
			h := userCommandHost(t, test.shell)
			link := filepath.Join(h.layout.Home, ".local/bin/agent-firmware")
			profile := filepath.Join(h.layout.Home, test.profile)
			if err := h.install("version one"); err != nil {
				t.Fatal(err)
			}
			if target, err := os.Readlink(link); err != nil || target != h.layout.Wrapper {
				t.Fatalf("short command target=%q, err=%v", target, err)
			}
			before := h.read(profile)
			if !strings.Contains(before, ".local/bin") {
				t.Fatal("missing shell PATH setup")
			}
			if shell != "" {
				checkShellLookup(t, shell, h.layout.Home, profile, link)
			}
			if err := h.install("version two"); err != nil {
				t.Fatal(err)
			}
			if h.read(link) != "version two" || h.read(profile) != before {
				t.Fatal("short command did not follow upgrade or profile was edited twice")
			}
			target, err := InspectRemovalHost(t.Context(), h.layout, h, false)
			if err != nil {
				t.Fatal(err)
			}
			if err := target.Remove(t.Context()); err != nil {
				t.Fatal(err)
			}
			if lexists(link) || h.read(profile) != before {
				t.Fatal("decommission left the command or changed the profile")
			}
			if shell != "" {
				checkShellLookup(t, shell, h.layout.Home, profile, "")
			}
		})
	}
}

func checkShellLookup(t *testing.T, shell, home, profile, want string) {
	t.Helper()
	var args []string
	switch filepath.Base(shell) {
	case "zsh":
		args = []string{"-f", "-c", `. "$1"; command -v agent-firmware`, "zsh", profile}
	case "bash":
		args = []string{"--noprofile", "--norc", "-c", `. "$1"; command -v agent-firmware`, "bash", profile}
	case "fish":
		args = []string{"--no-config", "-c", "source $argv[1]; command -v agent-firmware; or exit 1", profile}
	}
	cmd := exec.Command(shell, args...)
	cmd.Env = []string{"HOME=" + home, "PATH=/usr/bin:/bin"}
	output, err := cmd.CombinedOutput()
	if want == "" {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 || len(output) != 0 {
			t.Fatalf("command still visible after removal: %q, %v", output, err)
		}
	} else if err != nil || strings.TrimSpace(string(output)) != want {
		t.Fatalf("short command lookup=%q, err=%v, want=%q", output, err, want)
	}
}

func TestUserCommandKeepsPathOptOutAcrossUpdates(t *testing.T) {
	h := userCommandHost(t, "/bin/zsh")
	t.Setenv("AGENT_FIRMWARE_NO_MODIFY_PATH", "1")
	if err := h.install("version one"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_FIRMWARE_NO_MODIFY_PATH", "")
	if err := h.install("version two"); err != nil {
		t.Fatal(err)
	}
	if h.read(filepath.Join(h.layout.Home, ".local/bin/agent-firmware")) != "version two" ||
		lexists(filepath.Join(h.layout.Home, ".zshrc")) {
		t.Fatal("opt-out changed the profile or prevented installation of the command")
	}
	target, err := InspectRemovalHost(t.Context(), h.layout, h, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := target.Remove(t.Context()); err != nil {
		t.Fatal(err)
	}
	if lexists(filepath.Join(filepath.Dir(h.layout.Wrapper), ".cli-path-disabled")) {
		t.Fatal("decommission left the PATH preference")
	}
}

func TestUserCommandLeavesAnExistingPathAndSymlinkedProfileAlone(t *testing.T) {
	h := userCommandHost(t, "/bin/zsh")
	config := filepath.Join(t.TempDir(), "profile")
	const original = "# user-owned configuration\n"
	if err := os.WriteFile(config, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(h.layout.Home, ".zshrc")
	if err := os.Symlink(config, profile); err != nil {
		t.Fatal(err)
	}
	if err := h.install("version one"); err != nil || h.read(config) != original || !isLink(profile) {
		t.Fatalf("install touched a symlinked profile or failed: %v", err)
	}
	if err := os.Remove(profile); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Join(h.layout.Home, ".local/bin")+":/usr/bin:/bin")
	if err := h.install("version two"); err != nil || lexists(profile) {
		t.Fatalf("upgrade with a configured PATH edited the profile or failed: %v", err)
	}
}

func TestUserCommandPreservesForeignExecutable(t *testing.T) {
	h := userCommandHost(t, "/bin/zsh")
	dir := filepath.Join(h.layout.Home, ".local/bin")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "agent-firmware")
	if err := os.WriteFile(path, []byte("foreign command"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := h.install("version one"); err != nil {
		t.Fatal(err)
	}
	target, err := InspectRemovalHost(t.Context(), h.layout, h, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := target.Remove(t.Context()); err != nil {
		t.Fatal(err)
	}
	if h.read(path) != "foreign command" || lexists(filepath.Join(h.layout.Home, ".zshrc")) {
		t.Fatal("firmware changed the foreign command or enabled its directory")
	}
}

func TestFailedInstallLeavesShellUntouched(t *testing.T) {
	h := userCommandHost(t, "/bin/zsh")
	h.fail["start"] = true
	if err := h.install("version one"); !errors.Is(err, ErrStart) {
		t.Fatalf("install error=%v, want ErrStart", err)
	}
	if lexists(filepath.Join(h.layout.Home, ".local")) || lexists(filepath.Join(h.layout.Home, ".zshrc")) {
		t.Fatal("failed installation added a short command or changed the shell")
	}
}
