// Package workload owns the fixed device stack and its container transaction.
package workload

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"

	"github.com/amnezia-gpt/agent-firmware/internal/configuration"
	"github.com/amnezia-gpt/agent-firmware/internal/host"
)

const Manager = "agent-firmware"
const LabelPrefix = "org.amnezia.firmware."

type Error string

func (e Error) Error() string { return string(e) }

const (
	ErrEngine   Error = "engine_failed"
	ErrConflict Error = "resource_conflict"
	ErrHealth   Error = "workload_health_failed"
	ErrTimeout  Error = "engine_timeout"
	ErrCanceled Error = "operation_cancelled"
	ErrDeadline Error = "operation_deadline"
	ErrJournal  Error = "operation_journal_failed"
	ErrRecovery Error = "recovery_required"
	ErrCleanup  Error = "backup_cleanup_pending"
)

type Mount struct {
	Name, Target string
	ReadOnly     bool
}
type Binding struct {
	Source, Target string
	ReadOnly       bool
}
type Spec struct {
	Installation, Role, Name, Image, Network, Alias string
	Env                                             map[string]string
	Mounts                                          []Mount
	Bindings                                        []Binding `json:",omitempty"`
	ReadOnly                                        bool
	HealthStart, StopSeconds                        int
}

func (s Spec) Hash() string {
	b, _ := json.Marshal(s)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

type Stack struct {
	Installation, Generation, Network string
	Volumes                           []string
	Specs                             []Spec
}

func FromReaders(p, r io.Reader) (Stack, error) {
	i, err := configuration.Parse(p, r)
	if err != nil {
		return Stack{}, err
	}
	return FromInput(i), nil
}
func FromInput(input *configuration.Input) Stack {
	p, r := input.Profile(), input.Release()
	// Installation identity predates the API profile. Freeze its namespace salt;
	// compatibility upgrades must never allocate a second identity/data set.
	sum := sha256.Sum256([]byte("urn:amnezia-gpt:workload-contract:device:v1\n" + p.ID + "\n" + p.AuthIssuer))
	id := hex.EncodeToString(sum[:16])
	prefix := "af-" + id
	volume := func(role string) string { return prefix + "-" + role }
	stack := Stack{Installation: id, Generation: input.Generation(), Network: prefix + "-net"}
	for _, v := range []string{"identity", "gateway-state", "agent-home", "agent-workspace", "agent-auth", "storage-home", "storage-workspace", "storage-auth"} {
		stack.Volumes = append(stack.Volumes, volume(v))
	}
	stack.Specs = []Spec{
		{Installation: id, Role: "gateway", Name: prefix + "-gateway", Image: r.GatewayImage, Network: stack.Network, Alias: "amgpt-device-gateway", HealthStart: 5, StopSeconds: 15,
			Env:    map[string]string{"AMGPT_AUTH_ISSUER": p.AuthIssuer, "AMGPT_ROUTER_BASE_URL": p.RouterBaseURL, "AMGPT_RUNTIME_GATEWAY_BASE_URL": p.RuntimeGatewayBaseURL, "AMGPT_INGRESS_TRUST_BUNDLE": p.IngressTrustBundle, "AMGPT_INGRESS_TRUST_SHA256": p.IngressTrustSHA256, "AMGPT_DEVICE_IDENTITY_DIR": "/var/lib/amgpt-device-identity", "AMGPT_DEVICE_GATEWAY_STATE_DIR": "/var/lib/amgpt-device-gateway", "AMGPT_INGRESS_STATE_DIR": "/var/lib/amgpt-device-gateway/ingress", "AMGPT_CODEX_STORAGE_APP_URL": "ws://codex-storage:4500", "AMGPT_CODEX_STORAGE_APP_AUTH_DIR": "/run/amgpt-codex-storage"},
			Mounts: []Mount{{volume("identity"), "/var/lib/amgpt-device-identity", false}, {volume("gateway-state"), "/var/lib/amgpt-device-gateway", false}, {volume("agent-auth"), "/run/amgpt-codex", false}, {volume("storage-auth"), "/run/amgpt-codex-storage", false}}},
		{Installation: id, Role: "agent", Name: prefix + "-agent", Image: r.CodexImage, Network: stack.Network, Alias: "codex-app-server", ReadOnly: true, HealthStart: 60, StopSeconds: 20, Mounts: []Mount{{volume("agent-home"), "/home/codex/.codex", false}, {volume("agent-workspace"), "/codex-workspace", false}, {volume("agent-auth"), "/run/amgpt-codex", true}}},
		{Installation: id, Role: "storage", Name: prefix + "-storage", Image: r.CodexImage, Network: stack.Network, Alias: "codex-storage", ReadOnly: true, HealthStart: 60, StopSeconds: 20, Mounts: []Mount{{volume("storage-home"), "/home/codex/.codex", false}, {volume("storage-workspace"), "/codex-workspace", false}, {volume("storage-auth"), "/run/amgpt-codex", true}}},
	}
	if r.Contract == "urn:amnezia-gpt:workload-contract:device:v2" {
		gateway := &stack.Specs[0]
		gateway.Env["FIRMWARE_DEVICE_API_MODE"] = "required"
		gateway.Env["FIRMWARE_DEVICE_API_SOCKET"] = "/run/firmware-device-api/socket"
		gateway.Env["FIRMWARE_DEVICE_API_TOKEN_FILE"] = "/run/firmware-device-api-auth/token"
		gateway.Bindings = []Binding{{APIRuntimeDir(id) + "/api", "/run/firmware-device-api", true}, {APIRuntimeDir(id) + "/auth", "/run/firmware-device-api-auth", true}}
	}
	return stack
}

func APIRuntimeDir(installation string) string {
	return host.DefaultLayout().InstallationRuntime(installation)
}
