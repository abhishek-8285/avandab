package rag

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// failEmbedder simulates a dead embedding provider: preparation fails after
// chunking, which must NOT wipe the previously indexed results.
type failEmbedder struct{ dim int }

func (f *failEmbedder) Embed(ctx context.Context, text string) ([]float64, error) {
	return nil, errors.New("provider down")
}

func (f *failEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float64, error) {
	return nil, errors.New("provider down")
}

func (f *failEmbedder) Dimension() int { return f.dim }

func newOwnershipStore(t *testing.T) *VectorStore {
	t.Helper()
	store, err := NewVectorStore(filepath.Join(t.TempDir(), "vectors.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// Failed reindex preparation must leave old results searchable.
func TestReindexFailurePreservesOldResults(t *testing.T) {
	ctx := context.Background()
	store := newOwnershipStore(t)
	svc := NewService(NewHashEmbedder(16), store, 512, 50, "")

	if _, err := svc.Teach(ctx, "booking policy", "Cancellation requires 24 hours notice. Dispatcher confirms all bookings."); err != nil {
		t.Fatal(err)
	}
	seed, err := svc.Query(ctx, "cancellation policy", 5)
	if err != nil {
		t.Fatal(err)
	}
	if seed.Total == 0 {
		t.Fatal("seed content not searchable before reindex")
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "doc.txt"), []byte("fresh content for reindex"), 0644); err != nil {
		t.Fatal(err)
	}
	bad := NewService(&failEmbedder{dim: 16}, store, 512, 50, "")
	if _, err := bad.Reindex(ctx, dir); err == nil {
		t.Fatal("expected reindex error from dead provider")
	}

	after, err := svc.Query(ctx, "cancellation policy", 5)
	if err != nil {
		t.Fatal(err)
	}
	if after.Total == 0 {
		t.Error("failed reindex wiped old results: expected seed content still searchable")
	}
}

// Reindexing one directory must not wipe unrelated sources.
func TestReindexScopedToRequestedSourceOnly(t *testing.T) {
	ctx := context.Background()
	store := newOwnershipStore(t)
	svc := NewService(NewHashEmbedder(16), store, 512, 50, "")

	if _, err := svc.Teach(ctx, "booking policy", "Cancellation requires 24 hours notice for all bookings."); err != nil {
		t.Fatal(err)
	}
	dirA := t.TempDir()
	if err := os.WriteFile(filepath.Join(dirA, "alpha.txt"), []byte("alpha handbook personnel vacation rules"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.IndexDirectory(ctx, dirA); err != nil {
		t.Fatal(err)
	}
	dirB := t.TempDir()
	if err := os.WriteFile(filepath.Join(dirB, "beta.txt"), []byte("beta release notes deployment checklist"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.IndexDirectory(ctx, dirB); err != nil {
		t.Fatal(err)
	}

	// Reindex dirB with updated content.
	if err := os.WriteFile(filepath.Join(dirB, "beta.txt"), []byte("beta release notes updated with gamma rollout details"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Reindex(ctx, dirB); err != nil {
		t.Fatal(err)
	}

	all, err := store.Search(ctx, mustEmbed(svc, ctx, t, "handbook"), 100)
	if err != nil {
		t.Fatal(err)
	}
	sources := make(map[string]bool)
	var betaUpdated bool
	for _, e := range all {
		sources[e.Source] = true
		if strings.Contains(e.Source, "beta.txt") && strings.Contains(e.Content, "gamma") {
			betaUpdated = true
		}
	}
	var hasTeach, hasAlpha bool
	for s := range sources {
		if strings.HasPrefix(s, "teach/") {
			hasTeach = true
		}
		if strings.Contains(s, "alpha.txt") {
			hasAlpha = true
		}
	}
	if !hasTeach {
		t.Error("reindex of dirB wiped unrelated teach/ source")
	}
	if !hasAlpha {
		t.Error("reindex of dirB wiped unrelated dirA source")
	}
	if !betaUpdated {
		t.Error("reindex did not replace dirB content")
	}
}

func mustEmbed(svc *Service, ctx context.Context, t *testing.T, q string) []float64 {
	t.Helper()
	emb, err := svc.embedder.Embed(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	return emb
}

// countingRoundTripper fakes the embedding provider transport: no network,
// every provider call is counted so cancellation can be proven to stop them.
type countingRoundTripper struct {
	calls  int
	onCall func(call int)
}

func (f *countingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	f.calls++
	if f.onCall != nil {
		f.onCall(f.calls)
	}
	const body = `{"data":[{"embedding":[0.1,0.2,0.3]}]}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
	resp.Header.Set("Content-Type", "application/json")
	return resp, nil
}

func newFakeProviderEmbedder(fake http.RoundTripper) *OpenAIEmbedder {
	e := NewOpenAIEmbedder("test-key", "http://127.0.0.1:9", "test-model")
	e.client = &http.Client{Transport: fake}
	return e
}

// A cancelled embed batch must make no (further) provider calls.
func TestEmbedBatchCancelledMakesNoFurtherCalls(t *testing.T) {
	// Pre-cancelled: zero provider calls, ctx error out.
	fake := &countingRoundTripper{}
	e := newFakeProviderEmbedder(fake)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.EmbedBatch(ctx, []string{"a", "b", "c"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if fake.calls != 0 {
		t.Fatalf("cancelled batch made %d provider calls, want 0", fake.calls)
	}
	if _, err := e.Embed(ctx, "x"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if fake.calls != 0 {
		t.Fatalf("cancelled single embed made provider calls: %d", fake.calls)
	}

	// Cancelled mid-batch: stops after the in-flight call, no further calls.
	fake2 := &countingRoundTripper{}
	e2 := newFakeProviderEmbedder(fake2)
	ctx2, cancel2 := context.WithCancel(context.Background())
	fake2.onCall = func(call int) {
		if call == 1 {
			cancel2()
		}
	}
	if _, err := e2.EmbedBatch(ctx2, []string{"a", "b", "c", "d"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if fake2.calls != 1 {
		t.Fatalf("mid-batch cancel made %d provider calls, want exactly 1", fake2.calls)
	}
}
