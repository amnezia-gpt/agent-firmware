// Package dockerengine adapts the official SDK to the fixed workload executor.
package dockerengine

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/workload"
	"github.com/amnezia-gpt/agent-firmware/internal/workloadrelease"
	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"go.opentelemetry.io/otel/trace/noop"
)

type Engine struct {
	api         *client.Client
	transport   *http.Transport
	callTimeout time.Duration
	images      map[string]client.ImageInspectResult
}
type limitedBody struct {
	io.ReadCloser
	left int64
}

func (b *limitedBody) Read(p []byte) (int, error) {
	if b.left <= 0 {
		return 0, errors.New("engine_body_limit")
	}
	if int64(len(p)) > b.left {
		p = p[:b.left]
	}
	n, err := b.ReadCloser.Read(p)
	b.left -= int64(n)
	return n, err
}

type boundedTransport struct{ base *http.Transport }

func (t boundedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	limit := int64(2 * 1024 * 1024)
	if strings.HasSuffix(r.URL.Path, "/images/create") {
		limit = 32 * 1024 * 1024
	}
	response.Body = &limitedBody{response.Body, limit}
	return response, nil
}
func New(socket string) (*Engine, error) { return newClient(socket, 15*time.Second) }
func newClient(socket string, timeout time.Duration) (*Engine, error) {
	dialer := &net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, "unix", socket)
	}, MaxConnsPerHost: 4, MaxIdleConns: 4, MaxIdleConnsPerHost: 4, IdleConnTimeout: 30 * time.Second, ResponseHeaderTimeout: 15 * time.Second, MaxResponseHeaderBytes: 64 * 1024}
	api, err := client.New(client.WithHost("unix://"+socket), client.WithHTTPClient(&http.Client{Transport: boundedTransport{transport}}), client.WithTraceProvider(noop.NewTracerProvider()))
	if err != nil {
		transport.CloseIdleConnections()
		return nil, workload.ErrEngine
	}
	return &Engine{api, transport, timeout, map[string]client.ImageInspectResult{}}, nil
}
func (e *Engine) Close() { e.api.Close(); e.transport.CloseIdleConnections() }
func classify(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return workload.ErrTimeout
	}
	if errdefs.IsConflict(err) {
		return workload.ErrConflict
	}
	return workload.ErrEngine
}
func labels(id, role string) map[string]string {
	return map[string]string{workload.LabelPrefix + "managed-by": workload.Manager, workload.LabelPrefix + "installation": id, workload.LabelPrefix + "role": role}
}
func owned(values map[string]string, id, role string) bool {
	return values[workload.LabelPrefix+"managed-by"] == workload.Manager && values[workload.LabelPrefix+"installation"] == id && values[workload.LabelPrefix+"role"] == role
}

func (e *Engine) network(ctx context.Context, s workload.Stack, create bool) error {
	ctx, cancel := context.WithTimeout(ctx, e.callTimeout)
	defer cancel()
	result, err := e.api.NetworkInspect(ctx, s.Network, client.NetworkInspectOptions{})
	if errdefs.IsNotFound(err) {
		if !create {
			return nil
		}
		created, createErr := e.api.NetworkCreate(ctx, s.Network, client.NetworkCreateOptions{Driver: "bridge", Labels: labels(s.Installation, "network")})
		if createErr != nil {
			return classify(createErr)
		}
		result, err = e.api.NetworkInspect(ctx, created.ID, client.NetworkInspectOptions{})
	}
	if err != nil {
		return classify(err)
	}
	n := result.Network
	if n.Name != s.Network || !owned(n.Labels, s.Installation, "network") || n.Driver != "bridge" || n.Internal || n.Ingress || len(n.Options) > 0 {
		return workload.ErrConflict
	}
	for id, endpoint := range n.Containers {
		found := false
		for _, spec := range s.Specs {
			if endpoint.Name == spec.Name {
				found = true
			}
		}
		if !found && !slices.ContainsFunc(s.Retired, func(old workload.State) bool {
			return old.ID == id && old.Name == endpoint.Name
		}) {
			return workload.ErrConflict
		}
	}
	return nil
}
func (e *Engine) volumes(ctx context.Context, s workload.Stack, create bool) error {
	for _, name := range s.Volumes {
		call, cancel := context.WithTimeout(ctx, e.callTimeout)
		result, err := e.api.VolumeInspect(call, name, client.VolumeInspectOptions{})
		if errdefs.IsNotFound(err) {
			if create {
				created, createErr := e.api.VolumeCreate(call, client.VolumeCreateOptions{Name: name, Driver: "local", Labels: labels(s.Installation, "volume")})
				err = createErr
				if err == nil && (!owned(created.Volume.Labels, s.Installation, "volume") || created.Volume.Name != name || created.Volume.Driver != "local" || len(created.Volume.Options) > 0) {
					err = workload.ErrConflict
				}
			} else {
				err = nil
			}
		} else if err == nil {
			if !owned(result.Volume.Labels, s.Installation, "volume") || result.Volume.Driver != "local" || len(result.Volume.Options) > 0 {
				err = workload.ErrConflict
			}
		}
		cancel()
		if err != nil {
			var code workload.Error
			if errors.As(err, &code) {
				return code
			}
			return classify(err)
		}
	}
	return nil
}

// Retired finds only containers with this enrollment's ownership labels.
// Existing journals are recovered before listing, so backups are never adopted.
func (e *Engine) Retired(ctx context.Context, stack workload.Stack) ([]workload.State, error) {
	call, cancel := context.WithTimeout(ctx, e.callTimeout)
	list, err := e.api.ContainerList(call, client.ContainerListOptions{
		All: true, Limit: workloadrelease.MaxServices + 1,
		Filters: make(client.Filters).Add("label", workload.LabelPrefix+"installation="+stack.Installation),
	})
	cancel()
	if err != nil {
		return nil, classify(err)
	}
	if len(list.Items) > workloadrelease.MaxServices {
		return nil, workload.ErrConflict
	}
	var retired []workload.State
	for _, container := range list.Items {
		role := container.Labels[workload.LabelPrefix+"role"]
		name := "af-" + stack.Installation + "-" + role
		if !owned(container.Labels, stack.Installation, role) || !slices.Contains(container.Names, "/"+name) {
			return nil, workload.ErrConflict
		}
		if slices.ContainsFunc(stack.Specs, func(s workload.Spec) bool { return s.Role == role }) {
			continue
		}
		actual, err := e.Inspect(ctx, container.ID, workload.Spec{})
		if err != nil {
			return nil, err
		}
		if actual.ID != container.ID || actual.Name != name || actual.Installation != stack.Installation || actual.Role != role {
			return nil, workload.ErrConflict
		}
		retired = append(retired, actual)
	}
	slices.SortFunc(retired, func(a, b workload.State) int { return strings.Compare(a.Name, b.Name) })
	return retired, nil
}

func (e *Engine) Preflight(ctx context.Context, s workload.Stack) error {
	if err := e.network(ctx, s, false); err != nil {
		return err
	}
	if err := e.volumes(ctx, s, false); err != nil {
		return err
	}
	for _, spec := range s.Specs {
		call, cancel := context.WithTimeout(ctx, e.callTimeout)
		observed, err := e.api.ContainerInspect(call, spec.Name, client.ContainerInspectOptions{})
		cancel()
		if errdefs.IsNotFound(err) {
			continue
		}
		if err != nil {
			return classify(err)
		}
		actual := observed.Container
		identity := state(actual)
		if identity.Installation != spec.Installation || identity.Role != spec.Role || actual.HostConfig == nil || actual.Config == nil || actual.NetworkSettings == nil {
			return workload.ErrConflict
		}
		repository, digest, pinned := strings.Cut(actual.Config.Image, "@sha256:")
		if !pinned || repository == "" || len(digest) != 64 || strings.Trim(digest, "0123456789abcdef") != "" || len(actual.HostConfig.Binds) > 0 {
			return workload.ErrConflict
		}
		for name := range actual.NetworkSettings.Networks {
			if name != s.Network {
				return workload.ErrConflict
			}
		}
		for _, m := range actual.Mounts {
			if !knownMount(spec, m) {
				if err := e.checkRetainedMount(ctx, s.Installation, m); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
func (e *Engine) checkRetainedMount(ctx context.Context, installation string, m container.MountPoint) error {
	if m.Type != mount.TypeVolume || !strings.HasPrefix(m.Name, "af-"+installation+"-") {
		return workload.ErrConflict
	}
	call, cancel := context.WithTimeout(ctx, e.callTimeout)
	defer cancel()
	result, err := e.api.VolumeInspect(call, m.Name, client.VolumeInspectOptions{})
	if errdefs.IsNotFound(err) {
		return workload.ErrConflict
	}
	if err != nil {
		return classify(err)
	}
	v := result.Volume
	if v.Name != m.Name || !owned(v.Labels, installation, "volume") || v.Driver != "local" || len(v.Options) != 0 {
		return workload.ErrConflict
	}
	return nil
}

func (e *Engine) Prepare(ctx context.Context, s workload.Stack) error {
	for _, spec := range s.Specs {
		image, ok := e.images[spec.Image]
		if !ok {
			var err error
			image, err = e.prepareImage(ctx, spec.Image)
			if err != nil {
				return err
			}
			e.images[spec.Image] = image
		}
		if image.Os != "linux" || image.Architecture != "amd64" || image.Config == nil ||
			!validEffectiveUser(effectiveUser(spec, image.Config.User)) || image.Config.Healthcheck == nil ||
			len(image.Config.Healthcheck.Test) == 0 || image.Config.Healthcheck.Test[0] == "NONE" {
			return workload.ErrConflict
		}
		for target := range image.Config.Volumes {
			if !slices.ContainsFunc(spec.Mounts, func(m workload.Mount) bool { return m.Target == target }) {
				return workload.ErrConflict
			}
		}
	}
	if err := e.network(ctx, s, true); err != nil {
		return err
	}
	return e.volumes(ctx, s, true)
}

// PrepareImages downloads the release's distinct digest images without
// creating or resolving any enrollment resources.
func (e *Engine) PrepareImages(ctx context.Context, release workloadrelease.Release) error {
	for _, name := range release.Order() {
		reference := release.Services[name].Image.Reference()
		if _, ok := e.images[reference]; ok {
			continue
		}
		image, err := e.prepareImage(ctx, reference)
		if err != nil {
			return err
		}
		if image.Os != "linux" || image.Architecture != "amd64" || image.Config == nil {
			return workload.ErrConflict
		}
		e.images[reference] = image
	}
	return nil
}

func (e *Engine) prepareImage(ctx context.Context, reference string) (client.ImageInspectResult, error) {
	call, cancel := context.WithTimeout(ctx, e.callTimeout)
	image, err := e.api.ImageInspect(call, reference)
	cancel()
	if !errdefs.IsNotFound(err) {
		return image, classify(err)
	}

	pull, stop := context.WithTimeout(ctx, 3*time.Minute)
	defer stop()
	stream, err := e.api.ImagePull(pull, reference, client.ImagePullOptions{
		Platforms: []ocispec.Platform{{OS: "linux", Architecture: "amd64"}},
	})
	if err != nil {
		return client.ImageInspectResult{}, classify(err)
	}
	err = stream.Wait(pull)
	stream.Close()
	if err != nil {
		return client.ImageInspectResult{}, classify(err)
	}

	call, cancel = context.WithTimeout(ctx, e.callTimeout)
	defer cancel()
	image, err = e.api.ImageInspect(call, reference)
	return image, classify(err)
}

func validEffectiveUser(user string) bool {
	if !strings.ContainsRune(user, ':') {
		// Docker accepts numeric UID-only image users. An explicit UID keeps
		// image user-name resolution from selecting UID zero.
		user += ":1"
	}
	return workloadrelease.ValidUser(user)
}

func state(c container.InspectResponse) workload.State {
	s := workload.State{ID: c.ID, Name: strings.TrimPrefix(c.Name, "/")}
	if c.Config != nil {
		s.Installation = c.Config.Labels[workload.LabelPrefix+"installation"]
		s.Role = c.Config.Labels[workload.LabelPrefix+"role"]
		s.Hash = c.Config.Labels[workload.LabelPrefix+"spec"]
		s.Operation = c.Config.Labels[workload.LabelPrefix+"operation"]
		if c.Config.Labels[workload.LabelPrefix+"managed-by"] != workload.Manager {
			s.Installation = ""
		}
	}
	if c.HostConfig != nil {
		s.Policy = string(c.HostConfig.RestartPolicy.Name)
		if s.Policy == "on-failure" {
			s.Policy += ":" + strconv.Itoa(c.HostConfig.RestartPolicy.MaximumRetryCount)
		}
	}
	if c.State != nil {
		s.Running = c.State.Running && !c.State.Paused && !c.State.Restarting
		s.Healthy = c.State.Health != nil && c.State.Health.Status == "healthy"
	}
	return s
}
func (e *Engine) Inspect(ctx context.Context, name string, spec workload.Spec) (workload.State, error) {
	ctx, cancel := context.WithTimeout(ctx, e.callTimeout)
	defer cancel()
	result, err := e.api.ContainerInspect(ctx, name, client.ContainerInspectOptions{})
	if errdefs.IsNotFound(err) {
		return workload.State{}, nil
	}
	if err != nil {
		return workload.State{}, classify(err)
	}
	s := state(result.Container)
	image, ok := e.images[spec.Image]
	s.Matches = ok && matches(spec, result.Container, image)
	return s, nil
}
func env(values []string) map[string]string {
	out := map[string]string{}
	for _, v := range values {
		k, value, _ := strings.Cut(v, "=")
		out[k] = value
	}
	return out
}
func matches(s workload.Spec, c container.InspectResponse, image client.ImageInspectResult) bool {
	if c.Config == nil || c.HostConfig == nil || c.NetworkSettings == nil || image.Config == nil {
		return false
	}
	config, host := c.Config, c.HostConfig
	if !owned(config.Labels, s.Installation, s.Role) || config.Labels[workload.LabelPrefix+"spec"] != s.Hash() || c.Image != image.ID || config.Image != s.Image || config.User != effectiveUser(s, image.Config.User) || !reflect.DeepEqual([]string(config.Entrypoint), []string(image.Config.Entrypoint)) || !reflect.DeepEqual([]string(config.Cmd), []string(image.Config.Cmd)) || config.WorkingDir != image.Config.WorkingDir {
		return false
	}
	if host.Privileged || host.AutoRemove || host.ReadonlyRootfs != s.ReadOnly || len(host.CapAdd) != 0 || len(host.CapDrop) != 1 || host.CapDrop[0] != "ALL" || len(host.SecurityOpt) != 1 || (host.SecurityOpt[0] != "no-new-privileges" && host.SecurityOpt[0] != "no-new-privileges:true") || len(host.Binds) > 0 || len(host.VolumesFrom) > 0 || len(host.Devices) > 0 || len(host.DeviceRequests) > 0 || len(host.ExtraHosts) > 0 || len(host.Links) > 0 || len(host.PortBindings) > 0 || host.PublishAllPorts || host.NetworkMode != container.NetworkMode(s.Network) || host.RestartPolicy.Name != "unless-stopped" {
		return false
	}
	if host.PidMode != "" || host.UTSMode != "" || host.IpcMode == "host" || host.CgroupnsMode == "host" || len(host.Tmpfs) != 1 || host.Tmpfs["/tmp"] != "rw,noexec,nosuid,nodev" {
		return false
	}
	if config.StopTimeout == nil || *config.StopTimeout != s.StopSeconds || config.Healthcheck == nil || image.Config.Healthcheck == nil || !reflect.DeepEqual(config.Healthcheck.Test, image.Config.Healthcheck.Test) || config.Healthcheck.Interval != 10*time.Second || config.Healthcheck.Timeout != 5*time.Second || config.Healthcheck.Retries != 3 || config.Healthcheck.StartPeriod != time.Duration(s.HealthStart)*time.Second {
		return false
	}
	expected := env(image.Config.Env)
	for k, v := range s.Env {
		expected[k] = v
	}
	if !reflect.DeepEqual(env(config.Env), expected) {
		return false
	}
	if len(c.Mounts) != len(s.Mounts) {
		return false
	}
	for _, want := range s.Mounts {
		found := false
		for _, m := range c.Mounts {
			if m.Type == mount.TypeVolume && m.Name == want.Name && m.Destination == want.Target && m.RW != want.ReadOnly {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	if len(c.NetworkSettings.Networks) != 1 {
		return false
	}
	endpoint := c.NetworkSettings.Networks[s.Network]
	if endpoint == nil {
		return false
	}
	for _, alias := range s.Aliases {
		if !slices.Contains(endpoint.Aliases, alias) {
			return false
		}
	}
	for _, alias := range endpoint.Aliases {
		if !slices.Contains(s.Aliases, alias) && alias != s.Name && alias != c.ID && alias != c.ID[:min(len(c.ID), 12)] {
			return false
		}
	}
	return true
}

func (e *Engine) Create(ctx context.Context, s workload.Spec, op string) (workload.State, error) {
	ctx, cancel := context.WithTimeout(ctx, e.callTimeout)
	defer cancel()
	values := labels(s.Installation, s.Role)
	values[workload.LabelPrefix+"spec"] = s.Hash()
	values[workload.LabelPrefix+"operation"] = op
	var mounts []mount.Mount
	for _, m := range s.Mounts {
		mounts = append(mounts, mount.Mount{Type: mount.TypeVolume, Source: m.Name, Target: m.Target, ReadOnly: m.ReadOnly})
	}
	var environment []string
	for k, v := range s.Env {
		environment = append(environment, k+"="+v)
	}
	slices.Sort(environment)
	created, err := e.api.ContainerCreate(ctx, client.ContainerCreateOptions{Name: s.Name, Platform: &ocispec.Platform{OS: "linux", Architecture: "amd64"},
		Config:           &container.Config{Image: s.Image, User: s.User, Env: environment, Labels: values, StopTimeout: &s.StopSeconds, Healthcheck: &container.HealthConfig{Interval: 10 * time.Second, Timeout: 5 * time.Second, Retries: 3, StartPeriod: time.Duration(s.HealthStart) * time.Second}},
		HostConfig:       &container.HostConfig{NetworkMode: container.NetworkMode(s.Network), RestartPolicy: container.RestartPolicy{Name: "unless-stopped"}, ReadonlyRootfs: s.ReadOnly, CapDrop: []string{"ALL"}, SecurityOpt: []string{"no-new-privileges:true"}, Tmpfs: map[string]string{"/tmp": "rw,noexec,nosuid,nodev"}, Mounts: mounts},
		NetworkingConfig: &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{s.Network: {Aliases: s.Aliases}}}})
	if err != nil {
		return workload.State{}, classify(err)
	}
	actual, err := e.Inspect(ctx, s.Name, s)
	if err != nil {
		return workload.State{}, err
	}
	if actual.ID != created.ID {
		return workload.State{}, workload.ErrConflict
	}
	return actual, nil
}

func (e *Engine) Mutate(parent context.Context, expected workload.State, action, value string) error {
	budget := e.callTimeout
	if action == "stop" {
		budget = 120*time.Second + e.callTimeout
	}
	ctx, cancel := context.WithTimeout(parent, budget)
	defer cancel()
	actual, err := e.api.ContainerInspect(ctx, expected.ID, client.ContainerInspectOptions{})
	if err != nil {
		return classify(err)
	}
	if actual.Container.State == nil || actual.Container.Config == nil || actual.Container.HostConfig == nil {
		return workload.ErrEngine
	}
	current := state(actual.Container)
	if current.ID != expected.ID || current.Name != expected.Name || current.Installation != expected.Installation || current.Role != expected.Role {
		return workload.ErrConflict
	}
	switch action {
	case "start":
		if current.Running {
			return nil
		}
		_, err = e.api.ContainerStart(ctx, current.ID, client.ContainerStartOptions{})
	case "stop":
		if !actual.Container.State.Running {
			return nil
		}
		seconds := 20
		if configured := actual.Container.Config.StopTimeout; configured != nil && *configured >= 1 && *configured <= 120 {
			seconds = *configured
		}
		_, err = e.api.ContainerStop(ctx, current.ID, client.ContainerStopOptions{Timeout: &seconds})
	case "remove":
		_, err = e.api.ContainerRemove(ctx, current.ID, client.ContainerRemoveOptions{Force: true})
	case "rename":
		_, err = e.api.ContainerRename(ctx, current.ID, client.ContainerRenameOptions{NewName: value})
	case "policy":
		policy := container.RestartPolicy{Name: container.RestartPolicyMode(value)}
		if suffix, ok := strings.CutPrefix(value, "on-failure:"); ok {
			n, parseErr := strconv.Atoi(suffix)
			if parseErr != nil || n < 0 {
				return workload.ErrConflict
			}
			policy = container.RestartPolicy{Name: "on-failure", MaximumRetryCount: n}
		}
		if policy.Name != "no" && policy.Name != "always" && policy.Name != "unless-stopped" && policy.Name != "on-failure" {
			return workload.ErrConflict
		}
		_, err = e.api.ContainerUpdate(ctx, current.ID, client.ContainerUpdateOptions{RestartPolicy: &policy})
	default:
		return workload.ErrEngine
	}
	if err != nil {
		return classify(err)
	}
	check, err := e.api.ContainerInspect(ctx, current.ID, client.ContainerInspectOptions{})
	if action == "remove" && errdefs.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return classify(err)
	}
	now := state(check.Container)
	if action == "remove" || action == "start" && !now.Running || action == "stop" && check.Container.State.Running || action == "rename" && now.Name != value || action == "policy" && now.Policy != value {
		return workload.ErrEngine
	}
	return nil
}
func (e *Engine) Wait(parent context.Context, s workload.Spec, id string) error {
	budget := time.Duration(s.HealthStart)*time.Second + 90*time.Second
	ctx, cancel := context.WithTimeout(parent, budget)
	defer cancel()
	for {
		actual, err := e.Inspect(ctx, s.Name, s)
		if err != nil {
			return err
		}
		if actual.ID != id || actual.Installation != s.Installation || actual.Role != s.Role {
			return workload.ErrConflict
		}
		if actual.Running && actual.Healthy {
			return nil
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			if errors.Is(ctx.Err(), context.Canceled) {
				return context.Canceled
			}
			return workload.ErrHealth
		case <-timer.C:
		}
	}
}
