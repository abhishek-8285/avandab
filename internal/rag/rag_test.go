package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestChunker_ChunkFile(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")
	content := `package main

import "fmt"

func main() {
	fmt.Println("hello")
	fmt.Println("world")
	fmt.Println("test")
}
`
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	c := NewChunker(100, 10)
	chunks, err := c.ChunkFile(testFile)
	if err != nil {
		t.Fatal(err)
	}

	if len(chunks) == 0 {
		t.Error("expected at least one chunk")
	}

	for _, ch := range chunks {
		if ch.Content == "" {
			t.Error("chunk content should not be empty")
		}
		if ch.Source == "" {
			t.Error("chunk source should not be empty")
		}
	}
}

func TestChunker_ChunkText(t *testing.T) {
	c := NewChunker(50, 10)
	text := "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10"
	chunks := c.ChunkText(text, "test.md")

	if len(chunks) == 0 {
		t.Error("expected at least one chunk")
	}

	// Verify all chunks together cover the original text
	var reconstructed string
	for _, ch := range chunks {
		reconstructed += ch.Content + "\n"
	}

	if reconstructed == "" {
		t.Error("reconstructed text should not be empty")
	}
}

func TestChunker_IndexDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files
	files := map[string]string{
		"test.go":  "package main\n\nfunc main() {}\n",
		"doc.md":   "# Test\n\nSome documentation.\n",
		"skip.txt": "should be included",
	}

	for name, content := range files {
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	c := NewChunker(512, 50)
	chunks, err := c.IndexDirectory(tmpDir, []string{"go", "md", "txt"})
	if err != nil {
		t.Fatal(err)
	}

	if len(chunks) == 0 {
		t.Error("expected chunks from indexed directory")
	}
}

func TestCosineSimilarity(t *testing.T) {
	a := []float64{1.0, 0.0, 0.0}
	b := []float64{1.0, 0.0, 0.0}
	score := cosineSimilarity(a, b)
	if score < 0.99 || score > 1.01 {
		t.Errorf("identical vectors should have similarity ~1.0, got %f", score)
	}

	c := []float64{0.0, 1.0, 0.0}
	score = cosineSimilarity(a, c)
	if score > 0.01 {
		t.Errorf("orthogonal vectors should have similarity ~0.0, got %f", score)
	}

	// Different lengths
	d := []float64{1.0, 0.0}
	score = cosineSimilarity(a, d)
	if score != 0 {
		t.Errorf("different length vectors should have similarity 0, got %f", score)
	}
}

func TestVectorStore(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewVectorStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Add a chunk
	chunk := Chunk{
		ID:       "test#0",
		Content:  "hello world",
		Source:   "test.go",
		LineFrom: 1,
		LineTo:   2,
		ChunkIdx: 0,
	}
	embedding := []float64{1.0, 0.0, 0.0}

	if err := store.AddChunk(ctx, chunk, embedding); err != nil {
		t.Fatal(err)
	}

	count, err := store.Count(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("expected count 1, got %d", count)
	}

	// Search
	query := []float64{1.0, 0.0, 0.0}
	results, err := store.Search(ctx, query, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}

	// Clear
	if err := store.Clear(ctx); err != nil {
		t.Fatal(err)
	}
	count, err = store.Count(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("expected count 0 after clear, got %d", count)
	}
}

func TestVectorStore_BatchInsert(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewVectorStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	chunks := []Chunk{
		{ID: "test#0", Content: "hello", Source: "a.go", ChunkIdx: 0},
		{ID: "test#1", Content: "world", Source: "b.go", ChunkIdx: 0},
		{ID: "test#2", Content: "foo bar", Source: "c.go", ChunkIdx: 0},
	}
	embeddings := [][]float64{
		{1.0, 0.0, 0.0},
		{0.0, 1.0, 0.0},
		{0.0, 0.0, 1.0},
	}

	if err := store.AddChunks(ctx, chunks, embeddings); err != nil {
		t.Fatal(err)
	}

	count, err := store.Count(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Errorf("expected count 3, got %d", count)
	}
}

func TestHashEmbedder(t *testing.T) {
	embedder := NewHashEmbedder(384)

	emb1, err := embedder.Embed("hello world")
	if err != nil {
		t.Fatal(err)
	}

	if len(emb1) != 384 {
		t.Errorf("expected embedding dimension 384, got %d", len(emb1))
	}

	// Same input should produce same embedding
	emb2, err := embedder.Embed("hello world")
	if err != nil {
		t.Fatal(err)
	}

	for i := range emb1 {
		if emb1[i] != emb2[i] {
			t.Error("same input should produce same embedding")
			break
		}
	}

	// Different input should produce different embedding
	emb3, err := embedder.Embed("goodbye world")
	if err != nil {
		t.Fatal(err)
	}

	same := true
	for i := range emb1 {
		if emb1[i] != emb3[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("different inputs should produce different embeddings")
	}
}

func TestEmbedderBatch(t *testing.T) {
	embedder := NewHashEmbedder(128)

	texts := []string{"hello", "world", "foo", "bar", "baz"}
	embeddings, err := embedder.EmbedBatch(texts)
	if err != nil {
		t.Fatal(err)
	}

	if len(embeddings) != len(texts) {
		t.Errorf("expected %d embeddings, got %d", len(texts), len(embeddings))
	}

	for i, emb := range embeddings {
		if len(emb) != 128 {
			t.Errorf("embedding %d has wrong dimension: %d", i, len(emb))
		}
	}
}

func TestService_Query(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewVectorStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	embedder := NewHashEmbedder(64)
	svc := NewService(embedder, store, 512, 50, "")

	// Add some test content
	testDir := filepath.Join(tmpDir, "src")
	os.MkdirAll(testDir, 0755)
	os.WriteFile(filepath.Join(testDir, "main.go"), []byte(`package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}
`), 0644)
	os.WriteFile(filepath.Join(testDir, "utils.go"), []byte(`package main

// Helper functions
func add(a, b int) int {
	return a + b
}

func subtract(a, b int) int {
	return a - b
}
`), 0644)

	// Index
	count, err := svc.IndexDirectory(ctx, testDir)
	if err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("expected to index some chunks")
	}

	// Query
	result, err := svc.Query(ctx, "how to add numbers", 3)
	if err != nil {
		t.Fatal(err)
	}

	if result.Total == 0 {
		t.Error("expected search results")
	}

	if result.Query != "how to add numbers" {
		t.Errorf("expected query 'how to add numbers', got %q", result.Query)
	}
}

func TestService_Teach(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewVectorStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	embedder := NewHashEmbedder(64)
	svc := NewService(embedder, store, 512, 50, "")

	count, err := svc.Teach(ctx, "business rules", `
Our booking policy:
- All bookings must be confirmed by a dispatcher
- Cancellation requires 24 hours notice
- Premium customers get priority scheduling
- Peak hours are 9AM-6PM on weekdays
`)
	if err != nil {
		t.Fatal(err)
	}

	if count == 0 {
		t.Fatal("expected to teach some chunks")
	}

	// Query the taught content
	result, err := svc.Query(ctx, "what is the cancellation policy", 3)
	if err != nil {
		t.Fatal(err)
	}

	if result.Total == 0 {
		t.Error("expected to find taught content")
	}
}

func TestService_UploadFile(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	uploadDir := filepath.Join(tmpDir, "uploads")
	os.MkdirAll(uploadDir, 0755)

	store, err := NewVectorStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	embedder := NewHashEmbedder(64)
	svc := NewService(embedder, store, 512, 50, uploadDir)

	// Create a test text file
	testFile := filepath.Join(uploadDir, "policy.txt")
	content := `Employee Handbook v2.0

Section 1: Work Hours
- Standard hours: 9 AM to 6 PM
- Break: 1 hour lunch
- Overtime requires manager approval

Section 2: Holidays
- 12 paid holidays per year
- Must request time off 2 weeks in advance
`
	os.WriteFile(testFile, []byte(content), 0644)

	count, err := svc.UploadFile(ctx, testFile)
	if err != nil {
		t.Fatal(err)
	}

	if count == 0 {
		t.Fatal("expected to upload and index file")
	}

	// Query the uploaded content
	result, err := svc.Query(ctx, "what are the work hours", 3)
	if err != nil {
		t.Fatal(err)
	}

	if result.Total == 0 {
		t.Error("expected to find uploaded content")
	}
}

func TestExtractPDFText(t *testing.T) {
	// Simple PDF with text
	pdfContent := `1 0 0 1 100 700 cm
BT
/F1 12 Tf
100 0 Td
(This is a test PDF document.) Tj
0 -20 Td
(It contains multiple lines of text.) Tj
0 -20 Td
(Each line should be extracted.) Tj
ET
endobj`

	text, err := extractPDFText([]byte(pdfContent))
	if err != nil {
		t.Fatal(err)
	}

	if text == "" {
		t.Error("expected extracted text")
	}

	if !strings.Contains(text, "test PDF document") {
		t.Errorf("expected 'test PDF document' in text, got: %s", text)
	}
}

func TestExtractPDFText_Empty(t *testing.T) {
	_, err := extractPDFText([]byte("not a pdf"))
	if err == nil {
		t.Error("expected error for non-PDF content")
	}
}

func TestService_TeachFromFiles(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewVectorStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	embedder := NewHashEmbedder(64)
	svc := NewService(embedder, store, 512, 50, "")

	// Create test files
	file1 := filepath.Join(tmpDir, "policy1.txt")
	file2 := filepath.Join(tmpDir, "policy2.txt")
	os.WriteFile(file1, []byte("Booking must be confirmed by dispatcher.\nCancellation requires 24 hours notice."), 0644)
	os.WriteFile(file2, []byte("Premium customers get priority scheduling.\nPeak hours are 9AM-6PM."), 0644)

	count, err := svc.TeachFromFiles(ctx, "booking policies", []string{file1, file2})
	if err != nil {
		t.Fatal(err)
	}

	if count == 0 {
		t.Fatal("expected to teach some chunks")
	}

	// Query the taught content
	result, err := svc.Query(ctx, "what is the cancellation policy", 3)
	if err != nil {
		t.Fatal(err)
	}

	if result.Total == 0 {
		t.Error("expected to find taught content")
	}
}

func TestService_TeachFromDir(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewVectorStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	embedder := NewHashEmbedder(64)
	svc := NewService(embedder, store, 512, 50, "")

	// Create test directory with files
	docsDir := filepath.Join(tmpDir, "docs")
	os.MkdirAll(docsDir, 0755)
	os.WriteFile(filepath.Join(docsDir, "policy.md"), []byte("# Policy\n\nAll bookings need dispatcher confirmation."), 0644)
	os.WriteFile(filepath.Join(docsDir, "rules.txt"), []byte("Cancellation: 24 hours notice required.\nPremium: priority scheduling."), 0644)

	count, err := svc.TeachFromDir(ctx, "business rules", docsDir)
	if err != nil {
		t.Fatal(err)
	}

	if count == 0 {
		t.Fatal("expected to teach from directory")
	}

	// Query
	result, err := svc.Query(ctx, "what is the cancellation policy", 3)
	if err != nil {
		t.Fatal(err)
	}

	if result.Total == 0 {
		t.Error("expected to find taught content")
	}
}

func TestService_TeachFromDir_NoFiles(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewVectorStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	embedder := NewHashEmbedder(64)
	svc := NewService(embedder, store, 512, 50, "")

	// Empty directory
	_, err = svc.TeachFromDir(ctx, "empty", tmpDir)
	if err == nil {
		t.Error("expected error for directory with no supported files")
	}
}

func newTestHandler() *Handler {
	tmpDir, _ := os.MkdirTemp("", "rag-test-*")
	dbPath := filepath.Join(tmpDir, "test.db")
	store, _ := NewVectorStore(dbPath)
	embedder := NewHashEmbedder(64)
	svc := NewService(embedder, store, 512, 50, filepath.Join(tmpDir, "uploads"))
	return NewHandler(svc)
}

func postJSON(t *testing.T, h *Handler, path string, body map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	payload, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestHandler_DirectoryIndexRequiresAllowList(t *testing.T) {
	h := newTestHandler()
	allowed := t.TempDir()
	os.WriteFile(filepath.Join(allowed, "doc.txt"), []byte("hello"), 0644)
	h.WithAllowedDirs([]string{allowed})

	// Allowed directory → 200
	rec := postJSON(t, h, "/api/rag/index", map[string]string{"directory": allowed})
	if rec.Code != http.StatusOK {
		t.Fatalf("allowed dir: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Disallowed directory (system path) → 403
	rec = postJSON(t, h, "/api/rag/index", map[string]string{"directory": "/etc"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("disallowed dir /etc: expected 403, got %d", rec.Code)
	}

	// Parent traversal escape attempt → 403
	escaped := filepath.Join(allowed, "..", "..", "..")
	rec = postJSON(t, h, "/api/rag/index", map[string]string{"directory": escaped})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("path traversal: expected 403, got %d", rec.Code)
	}
}

func TestHandler_IndexFailsClosedWithoutAllowList(t *testing.T) {
	h := newTestHandler() // no WithAllowedDirs → empty allow-list → fail closed
	rec := postJSON(t, h, "/api/rag/index", map[string]string{"directory": "/tmp"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 (fail-closed) without allow-list, got %d", rec.Code)
	}
}

func TestHandler_SearchRemainsFunctional(t *testing.T) {
	h := newTestHandler()
	allowed := t.TempDir()
	os.WriteFile(filepath.Join(allowed, "doc.txt"), []byte("cancellation policy requires 24 hours"), 0644)
	h.WithAllowedDirs([]string{allowed})

	if rec := postJSON(t, h, "/api/rag/index", map[string]string{"directory": allowed}); rec.Code != http.StatusOK {
		t.Fatalf("index failed: %d", rec.Code)
	}

	rec := postJSON(t, h, "/api/rag/search", map[string]string{"query": "cancellation"})
	if rec.Code != http.StatusOK {
		t.Fatalf("search: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var res map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if total, _ := res["total"].(float64); total == 0 {
		t.Error("expected search results after indexing allowed dir")
	}
}
