package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/configuration"
	"github.com/amnezia-gpt/agent-firmware/internal/dockerengine"
	"github.com/amnezia-gpt/agent-firmware/internal/host"
	"github.com/amnezia-gpt/agent-firmware/internal/privatefile"
	"github.com/amnezia-gpt/agent-firmware/internal/service"
	"github.com/amnezia-gpt/agent-firmware/internal/workload"
)

const (
	newEnrollmentCommand = "Open the device's page in your account, copy the new installation command and run it here."
	checkService         = "Run 'sudo agent-firmware status' to check the firmware service and workloads."
)

type statusCondition struct {
	Type               string    `json:"type"`
	Status             string    `json:"status"`
	Reason             string    `json:"reason"`
	LastTransitionTime time.Time `json:"last_transition_time,omitzero"`
	LastProbeTime      time.Time `json:"last_probe_time,omitzero"`
}

type deviceStatus struct {
	SchemaVersion   int                       `json:"schema_version"`
	Phase           string                    `json:"phase"`
	Firmware        string                    `json:"firmware"`
	WorkloadRelease string                    `json:"workload_release,omitempty"`
	IDevID          string                    `json:"idevid_thumbprint,omitempty"`
	Issuer          string                    `json:"issuer,omitempty"`
	Conditions      []statusCondition         `json:"conditions"`
	UserData        []dockerengine.VolumeInfo `json:"user_data"`
	NextStep        string                    `json:"next_step"`
}

func phaseOf(snapshot configuration.Snapshot, now time.Time) (phase, next string) {
	switch {
	case snapshot.Decommissioning || snapshot.Removal != nil:
		return "removing", "repeat_removal_command"
	case snapshot.Device == nil:
		return "not_installed", "install"
	case snapshot.Enrollment == nil:
		return "manufactured", "enroll"
	}
	switch snapshot.Enrollment.Identity.Observed(now).Reason {
	case configuration.IdentityConfirmed:
		return "enrolled", "none"
	case configuration.IdentityLost:
		return "identity_lost", "new_enrollment_command"
	case configuration.IdentityRefused, configuration.IdentityRequired:
		return "enrollment_failed", "new_enrollment_command"
	case configuration.IdentityObtaining:
		return "enrolling", "wait"
	default:
		return "enrolled_unverified", "check_service"
	}
}

func (d deviceStatus) text() string {
	var out strings.Builder
	phase := map[string]string{
		"not_installed": "not installed", "manufactured": "manufactured and not enrolled",
		"enrolling": "enrolling", "enrollment_failed": "enrollment failed",
		"enrolled": "enrolled", "identity_lost": "identity lost",
		"enrolled_unverified": "enrolled (identity unverified)", "removing": "removing",
	}
	fmt.Fprintln(&out, "Device", phase[d.Phase])
	fmt.Fprintln(&out, "Firmware", d.Firmware)
	if d.WorkloadRelease != "" {
		fmt.Fprintln(&out, "Workload release", d.WorkloadRelease)
	}
	if d.IDevID != "" {
		fmt.Fprintln(&out, "IDevID", d.IDevID)
	}
	if d.Issuer != "" {
		fmt.Fprintln(&out, "Issuer", d.Issuer)
	}
	for _, condition := range d.Conditions {
		if condition.Type == "engine_ready" && condition.Reason == "waiting_for_engine" {
			fmt.Fprintln(&out, "Docker engine: waiting for Docker Desktop")
		}
	}
	switch {
	case d.UserData == nil:
		fmt.Fprintln(&out, "User data: unknown (local engine unavailable)")
	case len(d.UserData) == 0:
		fmt.Fprintln(&out, "User data: []")
	default:
		fmt.Fprintln(&out, "User data:")
		for _, volume := range d.UserData {
			issuer, created := volume.Issuer, volume.CreatedAt
			if issuer == "" {
				issuer = "unknown"
			}
			if created == "" {
				created = "unknown"
			}
			fmt.Fprintf(&out, "  %s  %s  %s\n", issuer, volume.Name, created)
		}
	}
	hint := map[string]string{
		"install": "Run the firmware installation command.", "enroll": "Run the installation command from your account to enroll this device.",
		"wait": "Wait for the domain to confirm the enrollment.", "new_enrollment_command": newEnrollmentCommand,
		"check_service":          "Check the firmware service with 'systemctl status agent-firmware'.",
		"repeat_removal_command": "Repeat the removal command to finish removing the device.",
	}
	if hostLayout.UserService {
		hint["check_service"] = "Check the firmware LaunchAgent in your login session and make sure Docker Desktop is running."
	}
	if line := hint[d.NextStep]; line != "" {
		fmt.Fprintln(&out, line)
	}
	return out.String()
}

func describeStatus(snapshot configuration.Snapshot, observed runtimeStatus, volumes []dockerengine.VolumeInfo, now time.Time) deviceStatus {
	phase, next := phaseOf(snapshot, now)
	doc := deviceStatus{SchemaVersion: 1, Phase: phase, Firmware: firmwareVersion, NextStep: next, UserData: volumes}
	doc.Conditions = []statusCondition{{Type: "manufactured", Status: "false", Reason: "device_record_missing"}}
	if snapshot.Device != nil {
		doc.WorkloadRelease, doc.IDevID = snapshot.Device.Release().Version, snapshot.Device.Thumbprint()
		doc.Conditions[0] = statusCondition{Type: "manufactured", Status: "true", Reason: "device_record_present"}
		doc.Conditions = append(doc.Conditions, statusCondition{Type: "enrolled", Status: "false", Reason: "not_bound_to_domain"})
	}
	if e := snapshot.Enrollment; e != nil {
		doc.Issuer = e.Profile.AuthIssuer
		doc.Conditions[1] = statusCondition{Type: "enrolled", Status: "true", Reason: "bound_to_domain", LastTransitionTime: e.BoundAt}
		identity := e.Identity.Observed(now)
		doc.Conditions = append(doc.Conditions, statusCondition{Type: "identity_valid", Status: identity.Status, Reason: string(identity.Reason),
			LastTransitionTime: identity.LastTransitionTime, LastProbeTime: identity.LastProbeTime})
		doc.Conditions = append(doc.Conditions, observed.conditions(snapshot, now)...)
	}
	if snapshot.Device != nil && snapshot.Removal == nil && observed.Generation == snapshot.Device.Generation() &&
		observed.Engine != nil && !now.Before(observed.Engine.LastProbeTime) && now.Sub(observed.Engine.LastProbeTime) <= 2*time.Minute {
		doc.Conditions = append(doc.Conditions, *observed.Engine)
	}
	return doc
}

var listStatusData = func(ctx context.Context, store configuration.Store) ([]dockerengine.VolumeInfo, error) {
	address, err := store.EngineAddress()
	if err != nil {
		return nil, err
	}
	engine, err := dockerengine.New(address)
	if err != nil {
		return nil, err
	}
	defer engine.Close()
	return engine.RetainedData(ctx, "")
}

func showStatus(store configuration.Store, stdout, stderr io.Writer) int {
	return renderStatus(store, false, stdout, stderr)
}

func renderStatus(store configuration.Store, asJSON bool, stdout, stderr io.Writer) int {
	snapshot, err := localState(store)
	if err != nil {
		return report(stderr, err.Error(), 1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	volumes, err := listStatusData(ctx, store)
	if err != nil {
		volumes = nil
	} else if volumes == nil {
		volumes = []dockerengine.VolumeInfo{}
	}
	for i := range volumes {
		if created, err := time.Parse(time.RFC3339Nano, volumes[i].CreatedAt); err == nil {
			volumes[i].CreatedAt = created.UTC().Format(time.RFC3339)
		}
	}
	var observed runtimeStatus
	if store.Path == defaultStateDir {
		observed = readRuntimeStatus(hostLayout)
	}
	doc := describeStatus(snapshot, observed, volumes, time.Now())
	if asJSON {
		return respond(stdout, stderr, doc)
	}
	return writeHelp(stdout, stderr, doc.text())
}

type runtimeStatus struct {
	service.Readiness
	Generation string           `json:"generation"`
	Binding    string           `json:"binding"`
	BoundAt    time.Time        `json:"bound_at"`
	Workloads  statusCondition  `json:"workloads"`
	Link       statusCondition  `json:"link"`
	Engine     *statusCondition `json:"engine,omitempty"`
}

func observeEngine(err error, now time.Time) *statusCondition {
	condition := &statusCondition{Type: "engine_ready", Status: "unknown", Reason: "not_observed", LastProbeTime: now}
	switch {
	case err == nil:
		condition.Status, condition.Reason = "true", "reachable"
	case errors.Is(err, workload.ErrEngineUnavailable):
		condition.Status, condition.Reason = "false", "waiting_for_engine"
	case errors.Is(err, workload.ErrEnginePrerequisites):
		condition.Status, condition.Reason = "false", workload.ErrEnginePrerequisites.Error()
	}
	return condition
}

func (r runtimeStatus) conditions(snapshot configuration.Snapshot, now time.Time) []statusCondition {
	if snapshot.Removal != nil || r.Generation != snapshot.Device.Generation() || r.Binding != workload.EnrollmentID(snapshot.Enrollment.Profile.AuthIssuer) ||
		!r.BoundAt.Equal(snapshot.Enrollment.BoundAt) || r.Workloads.LastProbeTime.IsZero() || now.Before(r.Workloads.LastProbeTime) ||
		now.Sub(r.Workloads.LastProbeTime) > configuration.IdentityObservationAge {
		return []statusCondition{{Type: "workloads_ready", Status: "unknown", Reason: "not_observed"},
			{Type: "link_ready", Status: "unknown", Reason: "not_observed"}}
	}
	return []statusCondition{r.Workloads, r.Link}
}

func readRuntimeStatus(layout host.Layout) runtimeStatus {
	owner := privatefile.EffectiveOwner()
	root, err := privatefile.OpenDirectory(layout.Runtime, owner, 0700)
	if err != nil {
		return runtimeStatus{}
	}
	defer root.Close()
	file, err := privatefile.OpenRegular(root, host.RuntimeStatus, os.O_RDONLY, 0600, owner, true)
	if err != nil {
		return runtimeStatus{}
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	var status runtimeStatus
	if err != nil || len(data) > 4096 || json.Unmarshal(data, &status) != nil {
		return runtimeStatus{}
	}
	return status
}

func writeRuntimeStatus(layout host.Layout, status *runtimeStatus) {
	root, err := privatefile.OpenDirectory(layout.Runtime, privatefile.EffectiveOwner(), 0700)
	if err != nil {
		return
	}
	defer root.Close()
	if status == nil {
		_ = root.Remove(host.RuntimeStatus)
		return
	}
	raw, err := json.Marshal(status)
	if err == nil {
		staged := host.RuntimeStatus + ".new"
		_ = root.Remove(staged)
		_, _ = privatefile.Replace(root, host.RuntimeStatus, staged, raw, 0600, false, nil, nil)
	}
}
