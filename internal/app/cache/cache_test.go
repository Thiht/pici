package cache

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/Thiht/pici/internal/app"
	"github.com/Thiht/pici/internal/docker"
	"github.com/Thiht/pici/internal/stores"
)

type fakeEngine struct {
	images  []docker.Image
	volumes []docker.Volume
	sizes   map[string]int64
	walks   int
	removed []string
	purged  bool
}

func (f *fakeEngine) Images(context.Context, string) ([]docker.Image, error) { return f.images, nil }

func (f *fakeEngine) Volumes(_ context.Context, namePrefix string) ([]docker.Volume, error) {
	var out []docker.Volume
	for _, volume := range f.volumes {
		if strings.HasPrefix(volume.Name, namePrefix) {
			out = append(out, volume)
		}
	}
	return out, nil
}

func (f *fakeEngine) VolumeUsage(context.Context) (map[string]int64, error) {
	f.walks++
	return f.sizes, nil
}

func (f *fakeEngine) RemoveImage(_ context.Context, reference string) error {
	f.removed = append(f.removed, reference)
	return nil
}

func (f *fakeEngine) RemoveVolume(_ context.Context, name string) error {
	f.removed = append(f.removed, name)
	return nil
}

func (f *fakeEngine) PurgeRegistry(context.Context) error {
	f.purged = true
	return nil
}

func registrySizes(size int64) map[string]int64 {
	sizes := make(map[string]int64, 2)
	for _, name := range docker.RegistryVolumeNames() {
		sizes[name] = size
	}
	return sizes
}

func project() stores.Project {
	return stores.Project{ID: uuid.MustParse("11111111-1111-1111-1111-111111111111"), Name: "demo"}
}

func TestGet(t *testing.T) {
	id := project().ID.String()
	created := time.Now()
	engine := &fakeEngine{
		images: []docker.Image{
			{Tags: []string{"other/image:latest"}, Size: 1},
			{Tags: []string{"pici/" + id + "-build:latest"}, Size: 100, Created: created, Containers: 2},
			{Tags: []string{"pici/" + id + "-test:latest"}, Size: 50, Created: created},
		},
		volumes: []docker.Volume{
			{Name: "pici-cache-" + id + "-node_modules", Created: created},
			{Name: "unrelated", Created: created},
		},
		sizes: map[string]int64{
			"pici-cache-" + id + "-node_modules": 10,
			"unrelated":                          999,
		},
	}

	summary, err := New(engine).Get(context.Background(), project())
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.Images) != 2 {
		t.Fatalf("expected the project's two images, got %+v", summary.Images)
	}
	if summary.Images[0].Reference != "pici/"+id+"-build:latest" {
		t.Fatalf("images should be sorted by reference, got %+v", summary.Images)
	}
	if summary.Images[0].Containers != 2 || !summary.Images[0].Created.Equal(created) {
		t.Fatalf("image fields were not carried over: %+v", summary.Images[0])
	}
	if summary.ImagesSize != 150 {
		t.Fatalf("ImagesSize = %d, want 150", summary.ImagesSize)
	}
	if len(summary.Volumes) != 1 || summary.Volumes[0].Cache != "node_modules" {
		t.Fatalf("unexpected volumes: %+v", summary.Volumes)
	}
	if summary.VolumesSize != 10 || summary.TotalSize != 160 {
		t.Fatalf("sizes = %d/%d, want 10/160", summary.VolumesSize, summary.TotalSize)
	}
}

func TestGetWithoutEngine(t *testing.T) {
	if _, err := New(nil).Get(context.Background(), project()); !errors.Is(err, app.ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
}

func TestRemove(t *testing.T) {
	id := project().ID.String()
	engine := &fakeEngine{}
	module := New(engine)
	ctx := context.Background()

	if err := module.RemoveImage(ctx, project(), "pici/"+id+"-build:latest"); err != nil {
		t.Fatal(err)
	}
	if err := module.RemoveVolume(ctx, project(), "pici-cache-"+id+"-node_modules"); err != nil {
		t.Fatal(err)
	}
	if len(engine.removed) != 2 {
		t.Fatalf("expected two removals, got %v", engine.removed)
	}
}

func TestRemoveForeignResource(t *testing.T) {
	module := New(&fakeEngine{})
	ctx := context.Background()

	if err := module.RemoveImage(ctx, project(), "pici/other-build:latest"); !errors.Is(err, app.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for a foreign image, got %v", err)
	}
	if err := module.RemoveVolume(ctx, project(), "pici-cache-other-node_modules"); !errors.Is(err, app.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for a foreign volume, got %v", err)
	}
}

func TestRegistry(t *testing.T) {
	ctx := context.Background()
	engine := &fakeEngine{sizes: registrySizes(2048)}
	m := New(engine)

	size, err := m.RegistrySize(ctx)
	if err != nil || size != 4096 {
		t.Fatalf("RegistrySize = %d, %v", size, err)
	}
	if err := m.PurgeRegistry(ctx); err != nil || !engine.purged {
		t.Fatalf("PurgeRegistry: purged=%v, err=%v", engine.purged, err)
	}

	unavailable := New(nil)
	if _, err := unavailable.RegistrySize(ctx); !errors.Is(err, app.ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
	if err := unavailable.PurgeRegistry(ctx); !errors.Is(err, app.ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
}

// A cache page must not pay for Docker's walk over every volume on each load.
func TestVolumeUsageIsCached(t *testing.T) {
	ctx := context.Background()
	engine := &fakeEngine{sizes: registrySizes(512)}
	m := New(engine)

	for range 3 {
		if _, err := m.Get(ctx, project()); err != nil {
			t.Fatal(err)
		}
		if _, err := m.RegistrySize(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if engine.walks != 1 {
		t.Fatalf("volume usage walked %d times, want 1", engine.walks)
	}
}

// Purging empties the registries, so the snapshot must not report their old
// size until the next refresh.
func TestPurgeForgetsRegistrySize(t *testing.T) {
	ctx := context.Background()
	engine := &fakeEngine{sizes: registrySizes(2048)}
	m := New(engine)

	if size, err := m.RegistrySize(ctx); err != nil || size != 4096 {
		t.Fatalf("RegistrySize = %d, %v", size, err)
	}
	if err := m.PurgeRegistry(ctx); err != nil {
		t.Fatal(err)
	}
	if size, err := m.RegistrySize(ctx); err != nil || size != 0 {
		t.Fatalf("RegistrySize after purge = %d, %v", size, err)
	}
	if engine.walks != 1 {
		t.Fatalf("purging walked the volumes again: %d walks", engine.walks)
	}
}
