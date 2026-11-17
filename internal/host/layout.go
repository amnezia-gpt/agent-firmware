// Package host defines the installed firmware's host layout and prerequisites.
package host

import "path/filepath"

const (
	MaxEngineAddress = 4096
	ServiceName      = "agent-firmware.service"
	LegacyState      = "accepted.json"
	DeviceState      = "device.json"
	EnrollmentDir    = "enrollment"
	EngineState      = "engine-address"
	RootSecret       = "root-secret"
	OperationJournal = "operation.json"
	OperationLock    = "operation.lock"
)

type Layout struct {
	Base            string
	Wrapper         string
	State           string
	Lock            string
	Runtime         string
	UpdateDirectory string
	EngineSocket    string
	Parents         []string
	UID, GID        uint32

	Unit           string
	DropIns        []string
	SystemdRuntime string
}

func DefaultLayout() Layout {
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

func (l Layout) InstallationRuntime(installation string) string {
	return filepath.Join(l.Runtime, installation)
}
