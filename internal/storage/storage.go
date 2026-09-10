// Package storage abstracts file/blob storage behind one interface so moving
// uploads from local disk to S3-compatible object storage is an env-only
// change (STORAGE_DRIVER), never a code change. Callers depend only on Store.
package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Driver names accepted by STORAGE_DRIVER.
const (
	DriverLocal = "local"
	DriverS3    = "s3" // reserved: requires an S3 client wiring; errors until then
)

// Store is the minimal contract every backend fulfils. Keys are logical
// paths ("logos/acme.png") — backends map them onto their own namespace.
type Store interface {
	// Save persists r under key and returns the storage key actually used.
	Save(ctx context.Context, key string, r io.Reader, contentType string) (string, error)
	// Open returns a reader for the stored bytes.
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	// Delete removes key. Deleting a missing key is not an error.
	Delete(ctx context.Context, key string) error
}

// Settings is the minimal view of config.StorageConfig this package needs.
type Settings interface {
	GetDriver() string
	GetLocalDir() string
}

// S3Settings provides S3/R2-specific configuration needed by DriverS3.
type S3Settings interface {
	GetS3Bucket() string
	GetS3Endpoint() string
	GetS3Region() string
	GetS3AccessKeyID() string
	GetS3SecretAccessKey() string
	GetS3PublicURL() string
}

// New builds the configured backend.
func New(cfg Settings) (Store, error) {
	switch strings.ToLower(cfg.GetDriver()) {
	case "", DriverLocal:
		dir := cfg.GetLocalDir()
		if dir == "" {
			return nil, fmt.Errorf("storage: LOCAL_STORAGE_DIR required for the local driver")
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("storage: mkdir %s: %w", dir, err)
		}
		return &localStore{root: dir}, nil
	case DriverS3:
		s3Cfg, ok := cfg.(S3Settings)
		if !ok {
			return nil, fmt.Errorf("storage: s3 driver requires S3Settings implementation")
		}
		return newS3(s3Cfg)
	default:
		return nil, fmt.Errorf("storage: unsupported driver %q (use local or s3)", cfg.GetDriver())
	}
}

// localStore keeps files under a root directory. Keys are sanitized to stop
// path traversal; nested keys create subdirectories on demand.
type localStore struct {
	root string
}

func (s *localStore) safePath(key string) (string, error) {
	clean := filepath.Clean(strings.TrimPrefix(key, "/"))
	// Reject empty keys and anything climbing out of the root ("..", "../x").
	if clean == "" || clean == "." || strings.HasPrefix(clean, "..") {
		return "", fmt.Errorf("storage: invalid key %q", key)
	}
	return filepath.Join(s.root, filepath.FromSlash(clean)), nil
}

func (s *localStore) Save(_ context.Context, key string, r io.Reader, _ string) (string, error) {
	path, err := s.safePath(key)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("storage: mkdir for %s: %w", path, err)
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".upload-*")
	if err != nil {
		return "", fmt.Errorf("storage: temp file: %w", err)
	}
	tmpName := f.Name()
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpName)
		return "", fmt.Errorf("storage: write %s: %w", key, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpName)
		return "", fmt.Errorf("storage: close %s: %w", key, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return "", fmt.Errorf("storage: finalize %s: %w", key, err)
	}
	return key, nil
}

func (s *localStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	path, err := s.safePath(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("storage: open %s: %w", key, err)
	}
	return f, nil
}

func (s *localStore) Delete(_ context.Context, key string) error {
	path, err := s.safePath(key)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("storage: delete %s: %w", key, err)
	}
	return nil
}
