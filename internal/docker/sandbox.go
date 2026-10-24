package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/docker/go-connections/nat"
)

// Labels pici puts on the containers it owns outside of a step, so the
// collector can find them again after a crash.
const (
	LabelSandbox  = "pici.sandbox"
	LabelProject  = "pici.project"
	LabelAttempt  = "pici.attempt"
	LabelRegistry = "pici.registry"
)

const (
	// DefaultSandboxImage runs the throwaway daemon of a docker workflow.
	DefaultSandboxImage = "docker:28.5.2-dind"
	// SandboxDaemonPort is the port the sandbox daemon listens on.
	SandboxDaemonPort = 2375
	// SandboxBIP is the inner bridge address of the sandbox. It must not
	// collide with the daemon's own bridge, which is where the sandbox finds
	// pici's registries.
	SandboxBIP = "10.201.0.1/16"
	// SandboxPool is the address pool the sandbox allocates user-defined
	// networks from, for compose projects that create their own.
	SandboxPool = "base=10.202.0.0/16,size=24"
)

const (
	registryImage         = "registry:3.1.2"
	registryImagesName    = "pici-registry"
	registryHubCacheName  = "pici-registry-cache"
	registryImagesVolume  = "pici-registry-images"
	registryHubCacheVol   = "pici-registry-hub-cache"
	registryImagesPort    = 5000
	registryHubCachePort  = 5001
	registryContainerPort = 5000
)

// Registries are the addresses the sandboxes pull from, reachable at the
// daemon's bridge gateway.
type Registries struct {
	Images   string
	HubCache string
}

// Daemon is a client to a Docker daemon, either the main one or the throwaway
// sandbox of an execution.
type Daemon interface {
	Run(ctx context.Context, opts RunOptions) (int, error)
	Pull(ctx context.Context, ref string, logs io.Writer) error
	Close() error
}

// SandboxOptions describes the throwaway daemon of a docker execution.
type SandboxOptions struct {
	Image     string
	RepoDir   string
	MountPath string
	Caches    []CacheMount
	Registry  string
	Mirror    string
	Labels    map[string]string
	// Addr is the host pici dials to reach the sandbox daemon. It is derived
	// from the daemon address when empty.
	Addr string
}

// CacheMount is a named cache volume mounted at the same path in the sandbox
// and in the step containers running inside it.
type CacheMount struct {
	Volume string
	Target string
}

// Sandbox identifies a created sandbox. Host is the address pici dials, StepHost
// the address the step containers dial from inside.
type Sandbox struct {
	ID       string
	Host     string
	StepHost string
}

// SandboxRef is a sandbox found on the daemon, used by the collector.
type SandboxRef struct {
	ID        string
	ProjectID string
	Attempt   int64
}

// NewEngineFor dials an explicit daemon instead of the one described by the
// environment.
func NewEngineFor(host string) (*Engine, error) {
	cli, err := client.NewClientWithOpts(client.WithHost(host), client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return &Engine{cli: cli}, nil
}

// Dial returns a client bound to the given daemon address.
func (e *Engine) Dial(host string) (Daemon, error) {
	return NewEngineFor(host)
}

// BridgeGateway is the address containers reach published ports at.
func (e *Engine) BridgeGateway(ctx context.Context) (string, error) {
	nw, err := e.cli.NetworkInspect(ctx, "bridge", network.InspectOptions{})
	if err != nil {
		return "", err
	}
	for _, cfg := range nw.IPAM.Config {
		if cfg.Gateway != "" {
			return cfg.Gateway, nil
		}
	}
	return "", errors.New("docker: bridge network has no gateway")
}

// CreateSandbox starts a throwaway daemon for one execution. Any sandbox
// already labelled with the same project and attempt is removed first, since a
// requeued execution reuses its attempt id.
func (e *Engine) CreateSandbox(ctx context.Context, opts SandboxOptions) (Sandbox, error) {
	if opts.Image == "" {
		opts.Image = DefaultSandboxImage
	}
	if err := e.ensureImage(ctx, opts.Image); err != nil {
		return Sandbox{}, err
	}
	// A unix daemon is reachable at pici's own loopback, whether pici runs next
	// to it or reaches it through a socket mount or a VM. A tcp daemon is only
	// reachable at the host pici already dials, so the port must be published on
	// every interface there.
	bindHost, dialHost := "127.0.0.1", "127.0.0.1"
	if daemonHost := e.cli.DaemonHost(); strings.HasPrefix(daemonHost, "tcp://") {
		bindHost = "0.0.0.0"
		if host, _, err := net.SplitHostPort(strings.TrimPrefix(daemonHost, "tcp://")); err == nil {
			dialHost = host
		}
	}
	if opts.Addr != "" {
		bindHost, dialHost = "0.0.0.0", opts.Addr
	}
	e.removeStaleSandboxes(ctx, opts.Labels)

	binds := []string{opts.RepoDir + ":" + opts.MountPath}
	for _, c := range opts.Caches {
		binds = append(binds, c.Volume+":"+c.Target)
	}

	daemonPort := nat.Port(strconv.Itoa(SandboxDaemonPort) + "/tcp")
	cmd := []string{
		"--host", fmt.Sprintf("tcp://0.0.0.0:%d", SandboxDaemonPort),
		"--bip", SandboxBIP,
		"--default-address-pool", SandboxPool,
	}
	if opts.Registry != "" {
		cmd = append(cmd, "--insecure-registry", opts.Registry)
	}
	if opts.Mirror != "" {
		cmd = append(cmd, "--insecure-registry", opts.Mirror, "--registry-mirror", "http://"+opts.Mirror)
	}

	created, err := e.cli.ContainerCreate(ctx,
		&container.Config{
			Image:  opts.Image,
			Cmd:    cmd,
			Env:    []string{"DOCKER_TLS_CERTDIR="},
			Labels: opts.Labels,
		},
		&container.HostConfig{
			Privileged: true,
			Binds:      binds,
			PortBindings: nat.PortMap{
				daemonPort: []nat.PortBinding{{HostIP: bindHost, HostPort: ""}},
			},
		},
		nil, nil, "")
	if err != nil {
		return Sandbox{}, err
	}
	id := created.ID
	if err := e.cli.ContainerStart(ctx, id, container.StartOptions{}); err != nil {
		_ = e.cli.ContainerRemove(context.Background(), id, container.RemoveOptions{Force: true})
		return Sandbox{}, err
	}

	insp, err := e.cli.ContainerInspect(ctx, id)
	if err != nil {
		_ = e.cli.ContainerRemove(context.Background(), id, container.RemoveOptions{Force: true})
		return Sandbox{}, err
	}
	port := ""
	if bindings := insp.NetworkSettings.Ports[daemonPort]; len(bindings) > 0 {
		port = bindings[0].HostPort
	}
	if port == "" {
		_ = e.cli.ContainerRemove(context.Background(), id, container.RemoveOptions{Force: true})
		return Sandbox{}, errors.New("docker: sandbox daemon did not bind a port")
	}

	innerGateway, _, _ := strings.Cut(SandboxBIP, "/")
	sandbox := Sandbox{
		ID:       id,
		Host:     "tcp://" + net.JoinHostPort(dialHost, port),
		StepHost: "tcp://" + net.JoinHostPort(innerGateway, strconv.Itoa(SandboxDaemonPort)),
	}
	if err := e.waitSandbox(ctx, sandbox.Host); err != nil {
		_ = e.cli.ContainerRemove(context.Background(), id, container.RemoveOptions{Force: true})
		return Sandbox{}, err
	}
	return sandbox, nil
}

func (e *Engine) waitSandbox(ctx context.Context, host string) error {
	deadline := time.Now().Add(time.Minute)
	for {
		engine, err := NewEngineFor(host)
		if err == nil {
			err = engine.Ping(ctx)
			_ = engine.Close()
			if err == nil {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("docker: sandbox daemon did not become ready: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func (e *Engine) removeStaleSandboxes(ctx context.Context, labels map[string]string) {
	project, attempt := labels[LabelProject], labels[LabelAttempt]
	if project == "" || attempt == "" {
		return
	}
	f := filters.NewArgs(
		filters.Arg("label", LabelSandbox+"=1"),
		filters.Arg("label", LabelProject+"="+project),
		filters.Arg("label", LabelAttempt+"="+attempt),
	)
	containers, err := e.cli.ContainerList(ctx, container.ListOptions{All: true, Filters: f})
	if err != nil {
		return
	}
	for _, c := range containers {
		slog.WarnContext(ctx, "removing stale sandbox", "id", c.ID, "project", project, "attempt", attempt)
		_ = e.cli.ContainerRemove(context.Background(), c.ID, container.RemoveOptions{Force: true, RemoveVolumes: true})
	}
}

// RemoveSandbox deletes a sandbox and everything it holds.
func (e *Engine) RemoveSandbox(ctx context.Context, id string) error {
	return e.cli.ContainerRemove(ctx, id, container.RemoveOptions{Force: true, RemoveVolumes: true})
}

// Sandboxes lists the sandboxes currently on the daemon.
func (e *Engine) Sandboxes(ctx context.Context) ([]SandboxRef, error) {
	f := filters.NewArgs(filters.Arg("label", LabelSandbox+"=1"))
	containers, err := e.cli.ContainerList(ctx, container.ListOptions{All: true, Filters: f})
	if err != nil {
		return nil, err
	}
	out := make([]SandboxRef, 0, len(containers))
	for _, c := range containers {
		attempt, err := strconv.ParseInt(c.Labels[LabelAttempt], 10, 64)
		if err != nil {
			continue
		}
		out = append(out, SandboxRef{ID: c.ID, ProjectID: c.Labels[LabelProject], Attempt: attempt})
	}
	return out, nil
}

// Pull fetches an image into the daemon behind this engine.
func (e *Engine) Pull(ctx context.Context, ref string, logs io.Writer) error {
	resp, err := e.cli.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		return err
	}
	defer resp.Close()
	if logs == nil {
		logs = io.Discard
	}
	return jsonmessage.DisplayJSONMessagesStream(resp, logs, 0, false, nil)
}

// Push uploads an image from the daemon behind this engine.
func (e *Engine) Push(ctx context.Context, ref string, logs io.Writer) error {
	resp, err := e.cli.ImagePush(ctx, ref, image.PushOptions{})
	if err != nil {
		return err
	}
	defer resp.Close()
	if logs == nil {
		logs = io.Discard
	}
	return jsonmessage.DisplayJSONMessagesStream(resp, logs, 0, false, nil)
}

func (e *Engine) ensureImage(ctx context.Context, ref string) error {
	if _, err := e.cli.ImageInspect(ctx, ref); err == nil {
		return nil
	}
	return e.Pull(ctx, ref, nil)
}

// EnsureRegistries starts the two registries pici owns: the one holding the
// workflow images it pushes, and the pull-through cache for Docker Hub.
func (e *Engine) EnsureRegistries(ctx context.Context) (Registries, error) {
	gateway, err := e.BridgeGateway(ctx)
	if err != nil {
		return Registries{}, err
	}
	if err := e.ensureImage(ctx, registryImage); err != nil {
		return Registries{}, err
	}
	if err := e.ensureRegistry(ctx, registryImagesName, registryImagesVolume, registryImagesPort, gateway, nil); err != nil {
		return Registries{}, err
	}
	if err := e.ensureRegistry(ctx, registryHubCacheName, registryHubCacheVol, registryHubCachePort, gateway, []string{
		"REGISTRY_PROXY_REMOTEURL=https://registry-1.docker.io",
	}); err != nil {
		return Registries{}, err
	}
	return Registries{
		Images:   net.JoinHostPort(gateway, strconv.Itoa(registryImagesPort)),
		HubCache: net.JoinHostPort(gateway, strconv.Itoa(registryHubCachePort)),
	}, nil
}

func (e *Engine) ensureRegistry(ctx context.Context, name, volumeName string, port int, gateway string, env []string) error {
	if _, err := e.cli.VolumeInspect(ctx, volumeName); err != nil {
		_, err := e.cli.VolumeCreate(ctx, volume.CreateOptions{
			Name:   volumeName,
			Labels: map[string]string{"pici": "1"},
		})
		// Another pici worker may have created it between the inspect and the
		// create; only fail when it is still missing.
		if _, inspectErr := e.cli.VolumeInspect(ctx, volumeName); inspectErr != nil {
			return err
		}
	}

	insp, err := e.cli.ContainerInspect(ctx, name)
	if err == nil && insp.Config != nil && insp.Config.Image == registryImage {
		if insp.State != nil && insp.State.Running {
			return nil
		}
		return e.cli.ContainerStart(ctx, name, container.StartOptions{})
	}
	if err == nil {
		// A pinned image change replaces the container but keeps its volume, so
		// the cache survives an upgrade.
		_ = e.cli.ContainerRemove(context.Background(), name, container.RemoveOptions{Force: true})
	}

	exposed := nat.Port(strconv.Itoa(registryContainerPort) + "/tcp")
	_, err = e.cli.ContainerCreate(ctx,
		&container.Config{
			Image:        registryImage,
			Env:          env,
			ExposedPorts: nat.PortSet{exposed: struct{}{}},
			Labels:       map[string]string{"pici": "1", LabelRegistry: name},
		},
		&container.HostConfig{
			Binds: []string{volumeName + ":/var/lib/registry"},
			PortBindings: nat.PortMap{
				exposed: []nat.PortBinding{{HostIP: gateway, HostPort: strconv.Itoa(port)}},
			},
			RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyUnlessStopped},
		},
		nil, nil, name)
	if err != nil {
		// A concurrent worker may have created the container first.
		if startErr := e.cli.ContainerStart(ctx, name, container.StartOptions{}); startErr == nil {
			return nil
		}
	}
	return err
}

// RegistryUsage is the on-disk size of the two registries pici owns.
func (e *Engine) RegistryUsage(ctx context.Context) (int64, error) {
	du, err := e.cli.DiskUsage(ctx, types.DiskUsageOptions{Types: []types.DiskUsageObject{types.VolumeObject}})
	if err != nil {
		return 0, err
	}
	var size int64
	for _, v := range du.Volumes {
		if v == nil || (v.Name != registryImagesVolume && v.Name != registryHubCacheVol) {
			continue
		}
		if v.UsageData != nil {
			size += v.UsageData.Size
		}
	}
	return size, nil
}

// PurgeRegistry drops both registries and starts them again empty. Workflow
// images are rebuilt and pushed again by the next execution that needs them.
func (e *Engine) PurgeRegistry(ctx context.Context) error {
	_ = e.cli.ContainerRemove(context.Background(), registryImagesName, container.RemoveOptions{Force: true})
	_ = e.cli.ContainerRemove(context.Background(), registryHubCacheName, container.RemoveOptions{Force: true})
	for _, name := range []string{registryImagesVolume, registryHubCacheVol} {
		if err := e.cli.VolumeRemove(context.Background(), name, true); err != nil {
			return err
		}
	}
	_, err := e.EnsureRegistries(ctx)
	return err
}
