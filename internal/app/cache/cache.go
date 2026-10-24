package cache

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Thiht/pici/internal/app"
	"github.com/Thiht/pici/internal/ci"
	"github.com/Thiht/pici/internal/docker"
	"github.com/Thiht/pici/internal/stores"
)

type Engine interface {
	Images(ctx context.Context, reference string) ([]docker.Image, error)
	Volumes(ctx context.Context, namePrefix string) ([]docker.Volume, error)
	RemoveImage(ctx context.Context, reference string) error
	RemoveVolume(ctx context.Context, name string) error
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
}

func New(engine Engine) *Module {
	return &Module{engine: engine}
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

	volumePrefix := ci.VolumePrefix(project.ID.String())
	volumes, err := m.engine.Volumes(ctx, volumePrefix)
	if err != nil {
		return Summary{}, err
	}
	for _, v := range volumes {
		summary.Volumes = append(summary.Volumes, Volume{
			Name:    v.Name,
			Cache:   strings.TrimPrefix(v.Name, volumePrefix),
			Size:    v.Size,
			Created: v.Created,
		})
		summary.VolumesSize += v.Size
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

func (m *Module) RemoveVolume(ctx context.Context, project stores.Project, name string) error {
	if !strings.HasPrefix(name, ci.VolumePrefix(project.ID.String())) {
		return fmt.Errorf("%w: unknown cache volume", app.ErrInvalid)
	}
	if m.engine == nil {
		return app.ErrUnavailable
	}
	return m.engine.RemoveVolume(ctx, name)
}
