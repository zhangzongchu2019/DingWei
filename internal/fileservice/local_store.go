package fileservice

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/sys/unix"
)

// LocalObjectStore keeps user-controlled names out of paths. Keys are created
// by Manager and are always interpreted relative to Root.
type LocalObjectStore struct {
	Root string
	// Test-only fault hook. Production constructors never set it.
	afterWrite func(string) error
}

var (
	legacyObjectKey = regexp.MustCompile(`^files/[0-9]{2}/[0-9]{4}/[0-9a-f]{32}-[0-9]{8}T[0-9]{6}\.[0-9]{9}Z$`)
	directObjectKey = regexp.MustCompile(`^[a-z0-9_-]{1,64}/` + sessionPathPattern + `/[0-9]{2}/[0-9]{4}/[0-9a-f]{32}\.(?:jpg|png|gif|webp|avif|heic|bmp|tiff|mp4|webm|ogv|mov|mkv|oga|wav|mp3|m4a|flac|aac|opus|pdf|txt|md|csv|json|log|py|js|go|java|yaml|yml|toml|ini|sh|sql|docx|xlsx|pptx|doc|xls|ppt|zip|bin)$`)
)

func validDeletableObjectKey(key string) bool {
	return key == filepath.ToSlash(filepath.Clean(filepath.FromSlash(key))) &&
		(legacyObjectKey.MatchString(key) || directObjectKey.MatchString(key))
}

func NewLocalObjectStore(root string) (*LocalObjectStore, error) {
	root = strings.TrimSpace(root)
	if root == "" || !filepath.IsAbs(root) {
		return nil, fmt.Errorf("FS_DATA_ROOT must be an absolute path")
	}
	if err := os.MkdirAll(root, os.ModeSetgid|0o750); err != nil {
		return nil, fmt.Errorf("create object root: %w", err)
	}
	if err := os.Chmod(root, os.ModeSetgid|0o750); err != nil {
		return nil, fmt.Errorf("set object root mode: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolve object root: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("object root is not a directory")
	}
	return &LocalObjectStore{Root: resolved}, nil
}

func (s *LocalObjectStore) path(key string) (string, error) {
	if s == nil || s.Root == "" || key == "" || filepath.IsAbs(key) {
		return "", fmt.Errorf("invalid local object key")
	}
	clean := filepath.Clean(filepath.FromSlash(key))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid local object key")
	}
	path := filepath.Join(s.Root, clean)
	rel, err := filepath.Rel(s.Root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("local object key escapes root")
	}
	return path, nil
}

func (s *LocalObjectStore) ensureDir(dir string) error {
	rel, err := filepath.Rel(s.Root, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("directory escapes object root")
	}
	current := s.Root
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		if err := os.Mkdir(current, os.ModeSetgid|0o750); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		info, err := os.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("object path component is not a real directory: %s", current)
		}
		if err := os.Chmod(current, os.ModeSetgid|0o750); err != nil {
			return fmt.Errorf("set object directory mode: %w", err)
		}
	}
	return nil
}

func (s *LocalObjectStore) Put(ctx context.Context, key string, body io.Reader, size int64, _ string) error {
	path, err := s.path(key)
	if err != nil || body == nil || size < 0 {
		return fmt.Errorf("invalid local put")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.ensureDir(filepath.Dir(path)); err != nil {
		return fmt.Errorf("create object directory: %w", err)
	}
	if _, err := os.Lstat(path); err == nil || !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("object target already exists")
	}
	tmp := filepath.Join(filepath.Dir(path), ".tmp-"+filepath.Base(path))
	fd, err := unix.Open(tmp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW, 0o640)
	if err != nil {
		return fmt.Errorf("create temporary object: %w", err)
	}
	f := os.NewFile(uintptr(fd), tmp)
	committed := false
	defer func() {
		_ = f.Close()
		if !committed {
			_ = os.Remove(tmp)
		}
	}()
	if err := unix.Fchmod(fd, 0o640); err != nil {
		return fmt.Errorf("set object mode: %w", err)
	}
	n, err := io.Copy(f, io.LimitReader(body, size+1))
	if err != nil || n != size {
		return fmt.Errorf("write complete object: wrote=%d want=%d: %w", n, size, err)
	}
	if s.afterWrite != nil {
		if err := s.afterWrite(tmp); err != nil {
			return err
		}
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("fsync object: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close object: %w", err)
	}
	if err := unix.Renameat2(unix.AT_FDCWD, tmp, unix.AT_FDCWD, path, unix.RENAME_NOREPLACE); err != nil {
		return fmt.Errorf("commit object: %w", err)
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("open object directory: %w", err)
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("fsync object directory: %w", err)
	}
	committed = true
	return nil
}

func (s *LocalObjectStore) Stat(_ context.Context, key string) (ObjectInfo, error) {
	path, err := s.path(key)
	if err != nil {
		return ObjectInfo{}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return ObjectInfo{}, err
	}
	if !info.Mode().IsRegular() {
		return ObjectInfo{}, fmt.Errorf("object is not a regular file")
	}
	return ObjectInfo{Size: info.Size()}, nil
}

func (s *LocalObjectStore) Open(_ context.Context, key string, offset, length int64) (io.ReadCloser, error) {
	path, err := s.path(key)
	if err != nil || offset < 0 || length <= 0 {
		return nil, fmt.Errorf("invalid local range")
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		f.Close()
		return nil, err
	}
	return struct {
		io.Reader
		io.Closer
	}{Reader: io.LimitReader(f, length), Closer: f}, nil
}

func (s *LocalObjectStore) Delete(_ context.Context, key string) error {
	if !validDeletableObjectKey(key) {
		return fmt.Errorf("refuse unsafe object delete")
	}
	path, err := s.path(key)
	if err != nil {
		return err
	}
	if info, statErr := os.Lstat(path); statErr == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refuse non-regular object delete")
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *LocalObjectStore) Move(_ context.Context, from, to string) error {
	src, err := s.path(from)
	if err != nil {
		return err
	}
	dst, err := s.path(to)
	if err != nil {
		return err
	}
	if err := s.ensureDir(filepath.Dir(dst)); err != nil {
		return err
	}
	if err := unix.Renameat2(unix.AT_FDCWD, src, unix.AT_FDCWD, dst, unix.RENAME_NOREPLACE); err != nil {
		return err
	}
	return nil
}
