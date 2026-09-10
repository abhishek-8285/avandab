package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type mockS3Client struct {
	objects     map[string][]byte
	types       map[string]string
	putObserver func(params *s3.PutObjectInput)
}

func newMockS3Client() *mockS3Client {
	return &mockS3Client{
		objects: make(map[string][]byte),
		types:   make(map[string]string),
	}
}

func (m *mockS3Client) PutObject(_ context.Context, params *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	data, err := io.ReadAll(params.Body)
	if err != nil {
		return nil, err
	}
	key := aws.ToString(params.Key)
	m.objects[key] = data
	if params.ContentType != nil {
		m.types[key] = *params.ContentType
	}
	return &s3.PutObjectOutput{}, nil
}

func (m *mockS3Client) GetObject(_ context.Context, params *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	key := aws.ToString(params.Key)
	data, ok := m.objects[key]
	if !ok {
		return nil, fmt.Errorf("NoSuchKey: %s", key)
	}
	return &s3.GetObjectOutput{
		Body: io.NopCloser(bytes.NewReader(data)),
	}, nil
}

func (m *mockS3Client) DeleteObject(_ context.Context, params *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	key := aws.ToString(params.Key)
	delete(m.objects, key)
	delete(m.types, key)
	return &s3.DeleteObjectOutput{}, nil
}

type fullS3Config struct {
	driver    string
	dir       string
	bucket    string
	endpoint  string
	region    string
	accessKey string
	secretKey string
	publicURL string
}

func (c *fullS3Config) GetDriver() string            { return c.driver }
func (c *fullS3Config) GetLocalDir() string          { return c.dir }
func (c *fullS3Config) GetS3Bucket() string          { return c.bucket }
func (c *fullS3Config) GetS3Endpoint() string        { return c.endpoint }
func (c *fullS3Config) GetS3Region() string          { return c.region }
func (c *fullS3Config) GetS3AccessKeyID() string     { return c.accessKey }
func (c *fullS3Config) GetS3SecretAccessKey() string { return c.secretKey }
func (c *fullS3Config) GetS3PublicURL() string       { return c.publicURL }

func TestS3Store_SaveOpenDelete(t *testing.T) {
	client := newMockS3Client()
	store := &s3Store{
		client: client,
		bucket: "test-bucket",
	}
	ctx := context.Background()

	// 1. Save with plain Reader
	key, err := store.Save(ctx, "pod/trip-123.jpg", strings.NewReader("photo-bytes"), "image/jpeg")
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	if key != "pod/trip-123.jpg" {
		t.Errorf("got key %q, want pod/trip-123.jpg", key)
	}
	if client.types[key] != "image/jpeg" {
		t.Errorf("got content-type %q, want image/jpeg", client.types[key])
	}

	// 2. Open and read
	rc, err := store.Open(ctx, "pod/trip-123.jpg")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	content, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}
	if string(content) != "photo-bytes" {
		t.Errorf("got %q, want photo-bytes", string(content))
	}

	// 3. Save with ReadSeeker
	seeker := bytes.NewReader([]byte("seeker-bytes"))
	key2, err := store.Save(ctx, "docs/rc.pdf", seeker, "application/pdf")
	if err != nil {
		t.Fatalf("Save with ReadSeeker failed: %v", err)
	}
	if key2 != "docs/rc.pdf" {
		t.Errorf("got key %q, want docs/rc.pdf", key2)
	}

	// 4. Delete
	if err := store.Delete(ctx, "pod/trip-123.jpg"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if _, ok := client.objects["pod/trip-123.jpg"]; ok {
		t.Error("object was not deleted")
	}

	// 5. Open deleted returns error
	if _, err := store.Open(ctx, "pod/trip-123.jpg"); err == nil {
		t.Error("Open on deleted object succeeded, want error")
	}
}

func TestS3Store_PathTraversalAndInvalidKeys(t *testing.T) {
	client := newMockS3Client()
	store := &s3Store{
		client: client,
		bucket: "test-bucket",
	}
	ctx := context.Background()

	invalidKeys := []string{
		"../../etc/passwd",
		"../secret",
		"",
		".",
		"/../escape",
	}

	for _, k := range invalidKeys {
		if _, err := store.Save(ctx, k, strings.NewReader("bad"), "text/plain"); err == nil {
			t.Errorf("Save(%q) did not return error", k)
		}
		if _, err := store.Open(ctx, k); err == nil {
			t.Errorf("Open(%q) did not return error", k)
		}
		if err := store.Delete(ctx, k); err == nil {
			t.Errorf("Delete(%q) did not return error", k)
		}
	}
}

// TestS3Store_SeekerStreams proves the ReadSeeker path sets ContentLength
// (seek-to-end) and hands the live reader to PutObject instead of buffering it.
// This is the regression guard for the file_service MultiReader bug that forced
// io.ReadAll and OOM'd on large uploads.
func TestS3Store_SeekerStreams(t *testing.T) {
	client := newMockS3Client()
	var gotLen *int64
	client.putObserver = func(params *s3.PutObjectInput) { gotLen = params.ContentLength }
	store := &s3Store{client: client, bucket: "b"}

	payload := []byte("streamed-payload")
	body := bytes.NewReader(payload) // implements io.ReadSeeker
	if _, err := store.Save(context.Background(), "a/b.bin", body, "application/octet-stream"); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	if gotLen == nil || *gotLen != int64(len(payload)) {
		t.Fatalf("seeker path must set ContentLength=%d, got %v", len(payload), gotLen)
	}
}

func TestNew_S3Validation(t *testing.T) {
	// Missing bucket
	cfg1 := &fullS3Config{
		driver:    "s3",
		accessKey: "test",
		secretKey: "test",
	}
	if _, err := New(cfg1); err == nil {
		t.Error("expected error for missing S3 bucket")
	}

	// Missing access key
	cfg2 := &fullS3Config{
		driver:    "s3",
		bucket:    "my-bucket",
		secretKey: "test",
	}
	if _, err := New(cfg2); err == nil {
		t.Error("expected error for missing S3 credentials")
	}

	// Missing secret key
	cfg3 := &fullS3Config{
		driver:    "s3",
		bucket:    "my-bucket",
		accessKey: "test",
	}
	if _, err := New(cfg3); err == nil {
		t.Error("expected error for missing S3 secret key")
	}
}
