package archive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type entry struct {
	name     string
	body     string
	typeflag byte
	link     string
	mode     int64
}

func tarGz(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Mode: e.mode, Size: int64(len(e.body))}
		if e.typeflag != 0 {
			hdr.Typeflag = e.typeflag
			hdr.Size = 0
		}
		if e.link != "" {
			hdr.Linkname = e.link
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if e.body != "" {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func extract(t *testing.T, data []byte) (string, error) {
	t.Helper()
	dest := t.TempDir()
	return dest, ExtractTarGz(bytes.NewReader(data), dest, 0)
}

func TestExtractRegularFile(t *testing.T) {
	dest, err := extract(t, tarGz(t, entry{name: "src/a.txt", body: "hello", mode: 0o644}))
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "src", "a.txt"))
	if err != nil || string(got) != "hello" {
		t.Fatalf("got %q err=%v", got, err)
	}
}

func TestExtractRejectsTraversal(t *testing.T) {
	if _, err := extract(t, tarGz(t, entry{name: "../evil", body: "x"})); err == nil {
		t.Fatal("expected traversal to be rejected")
	}
	if _, err := extract(t, tarGz(t, entry{name: "/etc/evil", body: "x"})); err == nil {
		t.Fatal("expected absolute path to be rejected")
	}
}

func TestExtractRejectsEscapingSymlink(t *testing.T) {
	data := tarGz(t, entry{name: "link", typeflag: tar.TypeSymlink, link: "../../outside"})
	if _, err := extract(t, data); err == nil {
		t.Fatal("expected escaping symlink to be rejected")
	}
}

func TestExtractAllowsInternalSymlink(t *testing.T) {
	data := tarGz(t,
		entry{name: "a.txt", body: "x"},
		entry{name: "link", typeflag: tar.TypeSymlink, link: "a.txt"},
	)
	dest, err := extract(t, data)
	if err != nil {
		t.Fatal(err)
	}
	target, err := os.Readlink(filepath.Join(dest, "link"))
	if err != nil || target != "a.txt" {
		t.Fatalf("readlink = %q err=%v", target, err)
	}
}

func TestExtractRejectsEscapingHardlink(t *testing.T) {
	for _, target := range []string{"../../outside", "/etc/passwd"} {
		data := tarGz(t,
			entry{name: "a.txt", body: "x", mode: 0o644},
			entry{name: "b.txt", typeflag: tar.TypeLink, link: target},
		)
		if _, err := extract(t, data); err == nil {
			t.Fatalf("expected escaping hardlink target %q to be rejected", target)
		}
	}
}

func TestExtractAllowsInternalHardlink(t *testing.T) {
	data := tarGz(t,
		entry{name: "a.txt", body: "hello", mode: 0o644},
		entry{name: "b.txt", typeflag: tar.TypeLink, link: "a.txt"},
	)
	dest, err := extract(t, data)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.txt", "b.txt"} {
		got, err := os.ReadFile(filepath.Join(dest, name))
		if err != nil || string(got) != "hello" {
			t.Fatalf("%s: got %q err=%v", name, got, err)
		}
	}
}

func TestExtractRejectsEntryThroughSymlinkParent(t *testing.T) {
	data := tarGz(t,
		entry{name: "dir", typeflag: tar.TypeDir, mode: 0o755},
		entry{name: "link", typeflag: tar.TypeSymlink, link: "dir"},
		entry{name: "link/file.txt", body: "x", mode: 0o644},
	)
	if _, err := extract(t, data); err == nil {
		t.Fatal("expected entry through a symlinked parent to be rejected")
	}
}

func TestExtractRejectsSpecialFiles(t *testing.T) {
	data := tarGz(t, entry{name: "dev", typeflag: tar.TypeChar})
	if _, err := extract(t, data); err == nil {
		t.Fatal("expected char device to be rejected")
	}
}

func TestExtractEnforcesMaxBytes(t *testing.T) {
	data := tarGz(t, entry{name: "big", body: strings.Repeat("a", 1024)})
	dest := t.TempDir()
	if err := ExtractTarGz(bytes.NewReader(data), dest, 64); err == nil {
		t.Fatal("expected size limit to be enforced")
	}
}

func TestExtractStripsSetuid(t *testing.T) {
	data := tarGz(t, entry{name: "bin/tool", body: "x", mode: 0o4755})
	dest, err := extract(t, data)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dest, "bin", "tool"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o4000 != 0 {
		t.Fatalf("setuid bit not stripped: %v", info.Mode())
	}
}

func TestExtractRejectsInvalidGzip(t *testing.T) {
	if _, err := extract(t, []byte("not gzip")); err == nil {
		t.Fatal("expected invalid gzip to be rejected")
	}
}
