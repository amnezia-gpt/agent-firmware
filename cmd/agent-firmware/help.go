package main

import (
	"fmt"
	"io"
	"strings"
)

type commandGroup uint8

const (
	lifecycleCommands commandGroup = iota
	checkCommands
	serviceCommands
)

type commandRunner func([]string, io.Reader, io.Writer, io.Writer) int

type commandSpec struct {
	name     string
	group    commandGroup
	synopsis string
	summary  string
	body     string
	flags    map[string]bool // true when the flag takes a value
	run      commandRunner
}

var firmwareCommands = []commandSpec{
	{"install", lifecycleCommands, "install --release <file>",
		"Manufacture the device on this host and install the service", `Manufacture the device and install the firmware service for the release.

Changes: Creates the root secret and IDevID, pulls images, installs the binary
and starts the service.
Keeps: The IDevID on repeat installation and any retained user data.
Requires: Root, Docker and a host without an earlier installation.
Example: sudo agent-firmware install --release workload-release.json
`, map[string]bool{"--release": true}, noInput(runInstall)},
	{"enroll", lifecycleCommands, "enroll --issuer <https-url>\n       [--enrollment-code-stdin | --enrollment-code-file <file>]",
		"Join a domain and start the workloads", `Join a domain and wait for the workload to report its enrollment. Read
the code from standard input or a file. The code itself stays out of arguments.

For an existing binding, the firmware checks whether the operational
identity is valid. With a lost identity, a fresh code for the same domain
renews the enrollment and keeps the keys, containers, network and volumes.
A working identity returns already_enrolled. If the workload cannot report
its state, the command returns workload_unavailable. Both leave the code unused.

Changes: Pins the domain profile and creates operational keys, the workload
network, volumes and containers on the first enrollment.
Keeps: The IDevID and retained user data of the same domain.
Requires: Root and an installed device eligible to join the domain.
Example: sudo agent-firmware enroll --issuer https://auth.example \
           --enrollment-code-file enrollment-code
`, map[string]bool{"--issuer": true, "--enrollment-code-stdin": false, "--enrollment-code-file": true}, runEnroll},
	{"status", lifecycleCommands, "status [--json] [--state-dir <directory>]",
		"Show the device, its enrollment and kept user data", `Show device and enrollment state and list kept user data from the local engine.
Use --json to print the versioned device state document.
Use --state-dir to select the firmware state directory to inspect.

Changes: None.
Requires: Root to read protected state.
Example: sudo agent-firmware status
`, map[string]bool{"--state-dir": true, "--json": false}, stateCommand("status")},
	{"update", lifecycleCommands, "update",
		"Install the latest firmware release", `Download and run agent-firmware-install.sh from the latest firmware release.

Changes: Replaces the firmware and the pinned workload release. Rolls back
the installation if the update fails.
Keeps: The device identity, enrollment and every volume.
Requires: Root and an installed device.
Example: sudo agent-firmware update
`, nil, noInput(runUpdate)},
	{"unenroll", lifecycleCommands, "unenroll --confirm [--delete-data]",
		"Leave the domain; user data is kept unless --delete-data", `Leave the current domain, with an attempt to deregister the device first.

Changes: Removes containers, the network, enrollment volumes, operational
keys and enrollment records. --delete-data also erases this domain's user data.
Keeps: The IDevID and other domains' data. By default, keeps this domain's data.
Requires: Root and --confirm. Without confirmation the command prints usage.
Example: sudo agent-firmware unenroll --confirm

Data removed with --delete-data cannot be recovered.
`, map[string]bool{"--confirm": false, "--delete-data": false}, noInput(runUnenroll)},
	{"decommission", lifecycleCommands, "decommission --confirm [--delete-data]",
		"Remove the device and the firmware; user data is kept\nunless --delete-data", `Remove the device and the installed firmware. If the binary is unavailable,
run the downloaded agent-firmware-install.sh:
  sudo sh agent-firmware-install.sh --decommission --confirm

Changes: Ends the enrollment and removes its resources, the device identity,
root secret, state, service and binaries. --delete-data also erases user data
of every domain on this host.
Keeps: Docker and, by default, user data.
An earlier installation's unclassified volumes also stay by default.
Those volumes cannot be attached and block a later install. To erase all
firmware volumes before reinstalling an earlier installation, run:
  sudo sh agent-firmware-install.sh --decommission --confirm --delete-data
Requires: Root and --confirm.
Example: sudo agent-firmware decommission --confirm

Data removed with --delete-data cannot be recovered.
`, map[string]bool{"--confirm": false, "--delete-data": false}, noInput(runDecommission)},
	{"discover-profile", checkCommands, "discover-profile --issuer <https-url>",
		"Assemble a domain's backend profile from its metadata", `Discover the issuer's metadata and print a backend profile to standard output.

Changes: None on the host.
Requires: Network access to the issuer.
Example: agent-firmware discover-profile --issuer https://auth.example
`, map[string]bool{"--issuer": true}, noInput(runDiscoverProfile)},
	{"validate-release", checkCommands, "validate-release --release <file> [--platform <os/arch>]",
		"Check a workload release manifest for this firmware", `Check the workload release schema, platform and Device API compatibility.
The platform defaults to the host's workload platform.
Use --platform to check a release build target.

Changes: None.
Requires: A readable workload release file.
Example: agent-firmware validate-release --release workload-release.json
`, map[string]bool{"--release": true, "--platform": true}, noInput(validateRelease)},
	{"validate-profile", checkCommands, "validate-profile < <file>",
		"Check a backend profile read from standard input", `Read and validate a backend profile from standard input.

Changes: None.
Requires: A backend profile on standard input.
Example: agent-firmware validate-profile < profile.json
`, nil, runValidateProfile},
	{"validate", checkCommands, "validate --profile <file> --release <file>",
		"Check a backend profile and a release together", `Validate a backend profile and a workload release as one configuration.

Changes: None.
Requires: Readable backend profile and workload release files.
Example: agent-firmware validate --profile profile.json --release release.json
`, map[string]bool{"--profile": true, "--release": true}, noInput(validateConfiguration)},
	{"serve", serviceCommands, "serve [--state-dir <directory>]", "", `Run by the service to supervise workloads and serve the local Device API.

Changes: Reconciles the recorded workload stack.
Requires: Root and installed device state.
Example: sudo agent-firmware serve
`, map[string]bool{"--state-dir": true}, stateCommand("serve")},
	{"apply", serviceCommands, "apply [--state-dir <directory>]", "", `Run one reconciliation from the recorded device state.

Changes: May reconcile resources for the current device state.
Requires: Root and installed device state.
Example: sudo agent-firmware apply
`, map[string]bool{"--state-dir": true}, stateCommand("apply")},
	{"preflight", serviceCommands, "preflight", "", `Run by the installer to check the local container engine.

Changes: None.
Requires: Root and a reachable local engine.
Example: sudo agent-firmware preflight
`, nil, noInput(runPreflight)},
}

func noInput(fn func([]string, io.Writer, io.Writer) int) commandRunner {
	return func(args []string, _ io.Reader, stdout, stderr io.Writer) int {
		return fn(args, stdout, stderr)
	}
}

func stateCommand(name string) commandRunner {
	return noInput(func(args []string, stdout, stderr io.Writer) int {
		return runStateCommand(append([]string{name}, args...), stdout, stderr)
	})
}

func findCommand(name string) *commandSpec {
	for i := range firmwareCommands {
		if firmwareCommands[i].name == name {
			return &firmwareCommands[i]
		}
	}
	return nil
}

func (cmd *commandSpec) acceptsFlags(args []string) bool {
	for i := 0; i < len(args); i++ {
		takesValue, ok := cmd.flags[args[i]]
		if !ok || takesValue && i+1 == len(args) {
			return false
		}
		if takesValue {
			i++
		}
	}
	return true
}

type commandDiagnostics struct {
	io.Writer
	command *commandSpec
}

func (d commandDiagnostics) hint() string {
	if d.command == nil {
		return "agent-firmware: run 'agent-firmware help' for the list of commands\n"
	}
	return "agent-firmware: run 'agent-firmware help " + d.command.name + "' for usage\n"
}

func helpFlag(arg string) bool { return arg == "--help" || arg == "-h" }

func run(args []string, input io.Reader, stdout, stderr io.Writer) int {
	diagnostics := commandDiagnostics{Writer: stderr}
	if len(args) == 0 {
		return report(diagnostics, "invalid_usage", 2)
	}
	if args[0] == "help" || helpFlag(args[0]) {
		switch {
		case len(args) == 1:
			return writeHelp(stdout, stderr, overview())
		case args[0] == "help" && len(args) == 2:
			if helpFlag(args[1]) {
				return writeHelp(stdout, stderr, overview())
			}
			if command := findCommand(args[1]); command != nil {
				return writeHelp(stdout, stderr, command.help())
			}
		}
		return report(diagnostics, "invalid_usage", 2)
	}
	command := findCommand(args[0])
	diagnostics.command = command
	args = args[1:]
	if command != nil && len(args) == 1 && helpFlag(args[0]) {
		return writeHelp(stdout, stderr, command.help())
	}
	if command == nil || !command.acceptsFlags(args) {
		return report(diagnostics, "invalid_usage", 2)
	}
	return command.run(args, input, stdout, diagnostics)
}

func writeHelp(stdout, stderr io.Writer, text string) int {
	if n, err := io.WriteString(stdout, text); err != nil || n != len(text) {
		return report(stderr, "output_failed", 1)
	}
	return 0
}

func (cmd *commandSpec) help() string {
	return "Usage: agent-firmware " + cmd.synopsis + "\n\n" + cmd.body
}

func overview() string {
	var out strings.Builder
	out.WriteString("agent-firmware <command> [flags]\n\nDevice lifecycle, in order:\n")
	writeGroup := func(group commandGroup, width int) {
		for _, command := range firmwareCommands {
			if command.group != group {
				continue
			}
			summary := strings.ReplaceAll(command.summary, "\n", "\n"+strings.Repeat(" ", 2+width))
			fmt.Fprintf(&out, "  %-*s%s\n", width, command.name, summary)
		}
	}
	writeGroup(lifecycleCommands, 15)
	out.WriteString("\nChecks that change nothing:\n")
	writeGroup(checkCommands, 19)
	out.WriteString("\nRun by the service and the installer, not by operators:\n  ")
	var serviceNames []string
	for _, command := range firmwareCommands {
		if command.group == serviceCommands {
			serviceNames = append(serviceNames, command.name)
		}
	}
	out.WriteString(strings.Join(serviceNames, ", "))
	out.WriteString("\n\nRun 'agent-firmware help <command>' for a command's flags and effects.\n")
	return out.String()
}
