package cache

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Thiht/pici/internal/app"
	"github.com/Thiht/pici/internal/ci"
	"github.com/Thiht/pici/internal/docker"
	"github.com/Thiht/pici/internal/stores"
)

// volumeUsageInterval is how often the module refreshes the volume sizes.
// Docker computes them by walking every volume, which takes tens of seconds
// once the caches grow.
const volumeUsageInterval = 5 * time.Minute

type Engine interface {
	Images(ctx context.Context, reference string) ([]docker.Image, error)
	Volumes(ctx context.Context, namePrefix string) ([]docker.Volume, error)
	VolumeUsage(ctx context.Context) (map[string]int64, error)
	RemoveImage(ctx context.Context, reference string) error
	RemoveVolume(ctx context.Context, name string) error
	PurgeRegistry(ctx context.Context) error
}

type Image struct {
	Reference  string    `json:"reference"`
	Size       int64     `json:"size"`
	Created    time.Time `json:"created"`
	Containers int64     `json:"containers"`
}

type Volume struct {
	Name    string    `json:"name"`
	Cache   string    `json:"cache"`
	Size    int64     `json:"size"`
	Created time.Time `json:"created"`
}

type Summary struct {
	Images      []Image
	Volumes     []Volume
	ImagesSize  int64
	VolumesSize int64
	TotalSize   int64
}

type Module struct {
	engine Engine

	mu    sync.Mutex
	sizes map[string]int64

	refreshMu sync.Mutex
}

func New(engine Engine) *Module {
	return &Module{engine: engine}
}

// Run keeps the volume sizes warm so the cache pages read a snapshot instead of
// waiting on Docker's disk usage walk.
func (m *Module) Run(ctx context.Context) {
	if m.engine == nil {
		return
	}
	m.refresh(ctx)
	ticker := time.NewTicker(volumeUsageInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.refresh(ctx)
		}
	}
}

func (m *Module) refresh(ctx context.Context) {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()
	walkCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	sizes, err := m.engine.VolumeUsage(walkCtx)
	if err != nil {
		slog.WarnContext(ctx, "read docker volume usage", "error", err)
		return
	}
	m.mu.Lock()
	m.sizes = sizes
	m.mu.Unlock()
}

// volumeSizes returns the last snapshot, walking the volumes once when no
// refresh succeeded yet.
func (m *Module) volumeSizes(ctx context.Context) (map[string]int64, error) {
	if sizes := m.cachedSizes(); sizes != nil {
		return sizes, nil
	}
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()
	if sizes := m.cachedSizes(); sizes != nil {
		return sizes, nil
	}
	walkCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	sizes, err := m.engine.VolumeUsage(walkCtx)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.sizes = sizes
	m.mu.Unlock()
	return sizes, nil
}

func (m *Module) cachedSizes() map[string]int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sizes
}

// forgetSizes drops volumes from the snapshot so a deletion shows up without
// waiting for the next refresh. It replaces the map instead of mutating it,
// since readers hold the current one.
func (m *Module) forgetSizes(names ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sizes == nil {
		return
	}
	sizes := make(map[string]int64, len(m.sizes))
	maps.Copy(sizes, m.sizes)
	for _, name := range names {
		delete(sizes, name)
	}
	m.sizes = sizes
}

// Get returns the Project's images and volumes, each sorted by name.
func (m *Module) Get(ctx context.Context, project stores.Project) (Summary, error) {
	if m.engine == nil {
		return Summary{}, app.ErrUnavailable
	}

	imagePrefix := ci.ImagePrefix(project.ID.String())
	images, err := m.engine.Images(ctx, imagePrefix+"*")
	if err != nil {
		return Summary{}, err
	}
	summary := Summary{Images: []Image{}, Volumes: []Volume{}}
	for _, img := range images {
		var reference string
		for _, tag := range img.Tags {
			if strings.HasPrefix(tag, imagePrefix) {
				reference = tag
				break
			}
		}
		if reference == "" {
			continue
		}
		summary.Images = append(summary.Images, Image{Reference: reference, Size: img.Size, Created: img.Created, Containers: img.Containers})
		summary.ImagesSize += img.Size
	}
	sort.Slice(summary.Images, func(i, j int) bool { return summary.Images[i].Reference < summary.Images[j].Reference })

	sizes, err := m.volumeSizes(ctx)
	if err != nil {
		return Summary{}, err
	}
	volumePrefix := ci.VolumePrefix(project.ID.String())
	volumes, err := m.engine.Volumes(ctx, volumePrefix)
	if err != nil {
		return Summary{}, err
	}
	for _, v := range volumes {
		size := sizes[v.Name]
		summary.Volumes = append(summary.Volumes, Volume{
			Name:    v.Name,
			Cache:   strings.TrimPrefix(v.Name, volumePrefix),
			Size:    size,
			Created: v.Created,
		})
		summary.VolumesSize += size
	}
	sort.Slice(summary.Volumes, func(i, j int) bool { return summary.Volumes[i].Cache < summary.Volumes[j].Cache })

	summary.TotalSize = summary.ImagesSize + summary.VolumesSize
	return summary, nil
}

func (m *Module) RemoveImage(ctx context.Context, project stores.Project, reference string) error {
	if !strings.HasPrefix(reference, ci.ImagePrefix(project.ID.String())) {
		return fmt.Errorf("%w: unknown image", app.ErrInvalid)
	}
	if m.engine == nil {
		return app.ErrUnavailable
	}
	return m.engine.RemoveImage(ctx, reference)
}

// RegistrySize is the on-disk size of the shared Docker Hub image cache.
func (m *Module) RegistrySize(ctx context.Context) (int64, error) {
	if m.engine == nil {
		return 0, app.ErrUnavailable
	}
	sizes, err := m.volumeSizes(ctx)
	if err != nil {
		return 0, err
	}
	var size int64
	for _, name := range docker.RegistryVolumeNames() {
		size += sizes[name]
	}
	return size, nil
}

// PurgeRegistry empties the shared Docker Hub image cache.
func (m *Module) PurgeRegistry(ctx context.Context) error {
	if m.engine == nil {
		return app.ErrUnavailable
	}
	if err := m.engine.PurgeRegistry(ctx); err != nil {
		return err
	}
	m.forgetSizes(docker.RegistryVolumeNames()...)
	return nil
}

func (m *Module) RemoveVolume(ctx context.Context, project stores.Project, name string) error {
	if !strings.HasPrefix(name, ci.VolumePrefix(project.ID.String())) {
		return fmt.Errorf("%w: unknown cache volume", app.ErrInvalid)
	}
	if m.engine == nil {
		return app.ErrUnavailable
	}
	if err := m.engine.RemoveVolume(ctx, name); err != nil {
		return err
	}
	m.forgetSizes(name)
	return nil
}
