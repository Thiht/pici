package archive

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxEntries = 1_000_000

// ExtractTarGz extracts a gzip-compressed tar stream into dest. Every entry is
// confined to dest: absolute paths, "..", escaping links and special files are
// rejected, a parent directory may not be a symlink, and the total decompressed
// size is bounded by maxBytes (4 GiB when unset).
func ExtractTarGz(src io.Reader, dest string, maxBytes int64) error {
	if maxBytes <= 0 {
		maxBytes = 4 << 30
	}
	gz, err := gzip.NewReader(src)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()

	lr := &io.LimitedReader{R: gz, N: maxBytes + 1}
	tr := tar.NewReader(lr)
	var entries int
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("tar: %w", err)
		}
		entries++
		if entries > maxEntries {
			return errors.New("archive has too many entries")
		}
		if err := extractEntry(tr, hdr, dest); err != nil {
			return fmt.Errorf("%s: %w", hdr.Name, err)
		}
	}
	if lr.N <= 0 {
		return errors.New("archive exceeds maximum uncompressed size")
	}
	return nil
}

func extractEntry(tr *tar.Reader, hdr *tar.Header, dest string) error {
	name := filepath.FromSlash(hdr.Name)
	if name == "" || name == "." || !filepath.IsLocal(name) {
		return errors.New("invalid entry path")
	}

	switch hdr.Typeflag {
	case tar.TypeDir:
		path, err := securePath(dest, name)
		if err != nil {
			return err
		}
		fi, err := os.Lstat(path)
		if err == nil {
			if !fi.IsDir() {
				return errors.New("directory entry over a non-directory")
			}
			return nil
		}
		return os.Mkdir(path, dirMode(hdr.Mode))
	case tar.TypeReg, tar.TypeRegA:
		path, err := securePath(dest, name)
		if err != nil {
			return err
		}
		return writeFile(tr, path, fileMode(hdr.Mode))
	case tar.TypeSymlink:
		return writeSymlink(dest, name, hdr.Linkname)
	case tar.TypeLink:
		return writeHardlink(dest, name, hdr.Linkname)
	case tar.TypeXGlobalHeader, tar.TypeXHeader:
		return nil
	default:
		return fmt.Errorf("unsupported entry type %d", hdr.Typeflag)
	}
}

// securePath creates the parent directories of name (if needed) and returns the
// target path. It refuses to traverse an existing symlink or non-directory.
func securePath(dest, name string) (string, error) {
	parts := strings.Split(filepath.Clean(name), string(filepath.Separator))
	cur := dest
	for _, part := range parts[:len(parts)-1] {
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(cur, 0o755); err != nil {
				return "", err
			}
			continue
		}
		if err != nil {
			return "", err
		}
		if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("invalid parent path component")
		}
	}
	return filepath.Join(cur, parts[len(parts)-1]), nil
}

func writeFile(r io.Reader, path string, mode os.FileMode) error {
	_ = os.Remove(path)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func writeSymlink(dest, name, link string) error {
	if filepath.IsAbs(filepath.FromSlash(link)) {
		return errors.New("absolute link target")
	}
	path, err := securePath(dest, name)
	if err != nil {
		return err
	}
	resolved := filepath.Clean(filepath.Join(filepath.Dir(path), filepath.FromSlash(link)))
	if resolved != dest && !strings.HasPrefix(resolved, dest+string(filepath.Separator)) {
		return errors.New("link target escapes destination")
	}
	_ = os.Remove(path)
	return os.Symlink(link, path)
}

func writeHardlink(dest, name, link string) error {
	linkName := filepath.FromSlash(link)
	if filepath.IsAbs(linkName) || !filepath.IsLocal(linkName) {
		return errors.New("invalid link target")
	}
	target := filepath.Join(dest, linkName)
	if target != dest && !strings.HasPrefix(target, dest+string(filepath.Separator)) {
		return errors.New("link target escapes destination")
	}
	path, err := securePath(dest, name)
	if err != nil {
		return err
	}
	_ = os.Remove(path)
	return os.Link(target, path)
}

func fileMode(m int64) os.FileMode { return os.FileMode(m).Perm() }

func dirMode(m int64) os.FileMode {
	mode := os.FileMode(m).Perm()
	if mode&0o700 == 0 {
		mode |= 0o700
	}
	return mode
}
