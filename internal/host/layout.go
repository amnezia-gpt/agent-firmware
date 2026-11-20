// Package host defines the installed firmware's host layout and prerequisites.
package host

import (
	"os"
	"path/filepath"
	"runtime"
)

const (
	MaxEngineAddress = 4096
	ServiceName      = "agent-firmware.service"
	LaunchAgentLabel = "org.amnezia.agent-firmware"
	LegacyState      = "accepted.json"
	DeviceState      = "device.json"
	EnrollmentDir    = "enrollment"
	EnrollmentRecord = "profile.json"
	EngineState      = "engine-address"
	RootSecret       = "root-secret"
	OperationJournal = "operation.json"
	OperationLock    = "operation.lock"
	RuntimeStatus    = "status.json"
)

type Layout struct {
	Base            string
	Wrapper         string
	State           string
	Lock            string
	Runtime         string
	UpdateDirectory string
	Home            string
	EngineSocket    string
	Parents         []string
	CreateParents   []string
	UID, GID        uint32
	UserService     bool

	Unit           string
	DropIns        []string
	SystemdRuntime string
}

func DefaultLayout() Layout {
	if runtime.GOOS == "darwin" {
		home, _ := os.UserHomeDir()
		return userLayout(home, uint32(os.Geteuid()), uint32(os.Getegid()))
	}
	const systemUnits = "/etc/systemd/system"
	const runtime = "/run"
	const runtimeUnits = runtime + "/systemd/system"
	return Layout{
		Base:            "/usr/local/lib/agent-firmware",
		Wrapper:         "/usr/local/bin/agent-firmware",
		Unit:            filepath.Join(systemUnits, ServiceName),
		State:           "/var/lib/agent-firmware",
		Lock:            runtime + "/agent-firmware-install",
		Runtime:         runtime + "/agent-firmware",
		UpdateDirectory: runtime,
		EngineSocket:    "/var/run/docker.sock",
		SystemdRuntime:  runtimeUnits,
		DropIns:         []string{filepath.Join(systemUnits, ServiceName+".d"), filepath.Join(runtimeUnits, ServiceName+".d")},
		Parents:         []string{"/usr", "/usr/local", "/usr/local/lib", "/usr/local/bin", "/etc", "/etc/systemd", systemUnits, "/var", "/var/lib", runtime},
	}
}

func userLayout(home string, uid, gid uint32) Layout {
	if !filepath.IsAbs(home) || filepath.Clean(home) != home || home == string(filepath.Separator) {
		return Layout{UID: uid, GID: gid, UserService: true}
	}
	library := filepath.Join(home, "Library")
	support := filepath.Join(library, "Application Support")
	root := filepath.Join(support, "agent-firmware")
	agents := filepath.Join(library, "LaunchAgents")
	return Layout{
		Base: filepath.Join(root, "firmware"), Wrapper: filepath.Join(root, "agent-firmware"),
		State: filepath.Join(root, "state"), Lock: filepath.Join(root, "install-lock"),
		Runtime: filepath.Join(root, "run"), UpdateDirectory: root,
		Unit: filepath.Join(agents, LaunchAgentLabel+".plist"), Home: home, UserService: true, UID: uid, GID: gid,
		Parents: []string{home, library, support, root, agents}, CreateParents: []string{library, support, root, agents},
	}
}

func (l Layout) InstallationRuntime(installation string) string {
	return filepath.Join(l.Runtime, installation)
}
