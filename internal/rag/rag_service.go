package rag

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type SearchResult struct {
	Chunks []VectorEntry `json:"chunks"`
	Scores []float64     `json:"scores"`
	Query  string        `json:"query"`
	Total  int           `json:"total"`
}

type Service struct {
	chunker    *Chunker
	embedder   Embedder
	store      *VectorStore
	extensions []string
	uploadDir  string
}

func NewService(embedder Embedder, store *VectorStore, chunkSize, chunkOverlap int, uploadDir string) *Service {
	return &Service{
		chunker:    NewChunker(chunkSize, chunkOverlap),
		embedder:   embedder,
		store:      store,
		uploadDir:  uploadDir,
		extensions: []string{"go", "md", "txt", "yaml", "yml", "json", "sql", "ts", "tsx", "js", "jsx", "html", "css", "toml"},
	}
}

func (s *Service) IndexDirectory(ctx context.Context, dirPath string) (int, error) {
	log.Printf("rag: indexing directory %s", dirPath)

	chunks, err := s.chunker.IndexDirectory(dirPath, s.extensions)
	if err != nil {
		return 0, fmt.Errorf("chunk files: %w", err)
	}

	if len(chunks) == 0 {
		return 0, nil
	}

	log.Printf("rag: generated %d chunks from %d files", len(chunks), countUniqueSources(chunks))

	embeddings, err := s.embedBatch(chunks)
	if err != nil {
		return 0, fmt.Errorf("embed chunks: %w", err)
	}

	if err := s.store.AddChunks(ctx, chunks, embeddings); err != nil {
		return 0, fmt.Errorf("store chunks: %w", err)
	}

	count, _ := s.store.Count(ctx)
	log.Printf("rag: indexed %d chunks (%d unique files)", count, countUniqueSources(chunks))
	return count, nil
}

func (s *Service) Teach(ctx context.Context, name, content string) (int, error) {
	if name == "" {
		name = "unteached"
	}
	source := "teach/" + strings.ReplaceAll(strings.ToLower(name), " ", "-") + ".txt"

	chunks := s.chunker.ChunkText(content, source)
	if len(chunks) == 0 {
		return 0, nil
	}

	log.Printf("rag: teaching %d chunks from %s", len(chunks), name)

	embeddings, err := s.embedBatch(chunks)
	if err != nil {
		return 0, fmt.Errorf("embed taught content: %w", err)
	}

	if err := s.store.AddChunks(ctx, chunks, embeddings); err != nil {
		return 0, fmt.Errorf("store taught chunks: %w", err)
	}

	count, _ := s.store.Count(ctx)
	log.Printf("rag: taught %s — now %d total chunks", name, count)
	return count, nil
}

// TeachFromFiles reads file content and teaches it to the RAG under a topic name.
// Returns total chunk count after teaching.
func (s *Service) TeachFromFiles(ctx context.Context, topic string, filePaths []string) (int, error) {
	if topic == "" {
		topic = "untitled"
	}

	var allChunks []Chunk

	for _, fp := range filePaths {
		data, err := os.ReadFile(fp)
		if err != nil {
			return 0, fmt.Errorf("read %s: %w", fp, err)
		}

		content := string(data)
		name := topic + "/" + filepath.Base(fp)
		chunks := s.chunker.ChunkText(content, name)
		allChunks = append(allChunks, chunks...)
	}

	if len(allChunks) == 0 {
		return 0, nil
	}

	log.Printf("rag: teaching %d chunks from %d files under topic %s", len(allChunks), len(filePaths), topic)

	embeddings, err := s.embedBatch(allChunks)
	if err != nil {
		return 0, fmt.Errorf("embed taught content: %w", err)
	}

	if err := s.store.AddChunks(ctx, allChunks, embeddings); err != nil {
		return 0, fmt.Errorf("store taught chunks: %w", err)
	}

	count, _ := s.store.Count(ctx)
	log.Printf("rag: taught %s — now %d total chunks", topic, count)
	return count, nil
}

// TeachFromDir teaches all supported files from a directory.
func (s *Service) TeachFromDir(ctx context.Context, topic string, dirPath string) (int, error) {
	var files []string
	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			base := strings.ToLower(info.Name())
			if base == "node_modules" || base == ".git" || base == "vendor" || base == "bin" || base == "dist" {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.TrimPrefix(filepath.Ext(path), ".")
		supported := map[string]bool{
			"md": true, "txt": true, "pdf": true,
			"go": true, "sql": true, "yaml": true, "yml": true,
			"json": true, "ts": true, "tsx": true, "js": true, "jsx": true,
			"html": true, "css": true, "toml": true,
		}
		if supported[ext] {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("walk dir: %w", err)
	}

	if len(files) == 0 {
		return 0, fmt.Errorf("no supported files found in %s", dirPath)
	}

	return s.TeachFromFiles(ctx, topic, files)
}

func (s *Service) UploadFile(ctx context.Context, filePath string) (int, error) {
	ext := strings.TrimPrefix(filepath.Ext(filePath), ".")
	supported := map[string]bool{
		"txt": true, "md": true, "pdf": true,
	}
	if !supported[ext] {
		return 0, fmt.Errorf("unsupported file type: .%s (supported: txt, md, pdf)", ext)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return 0, fmt.Errorf("read file: %w", err)
	}

	content := string(data)
	name := filepath.Base(filePath)

	if ext == "pdf" {
		content, err = extractPDFText(data)
		if err != nil {
			return 0, fmt.Errorf("extract PDF text: %w", err)
		}
	}

	return s.Teach(ctx, name, content)
}

func (s *Service) Query(ctx context.Context, query string, topK int) (*SearchResult, error) {
	if topK <= 0 {
		topK = 5
	}

	embedding, err := s.embedder.Embed(query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	chunks, err := s.store.Search(ctx, embedding, topK)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}

	scores := make([]float64, len(chunks))
	for i, c := range chunks {
		scores[i] = cosineSimilarity(embedding, c.Embedding)
	}

	return &SearchResult{
		Chunks: chunks,
		Scores: scores,
		Query:  query,
		Total:  len(chunks),
	}, nil
}

func (s *Service) Reindex(ctx context.Context, dirPath string) (int, error) {
	if err := s.store.Clear(ctx); err != nil {
		return 0, fmt.Errorf("clear store: %w", err)
	}
	return s.IndexDirectory(ctx, dirPath)
}

func (s *Service) Stats(ctx context.Context) (int, error) {
	return s.store.Count(ctx)
}

func (s *Service) embedBatch(chunks []Chunk) ([][]float64, error) {
	contentTexts := make([]string, len(chunks))
	for i, c := range chunks {
		contentTexts[i] = c.Content
	}

	batchSize := 100
	var allEmbeddings [][]float64

	for i := 0; i < len(contentTexts); i += batchSize {
		end := i + batchSize
		if end > len(contentTexts) {
			end = len(contentTexts)
		}

		batch := contentTexts[i:end]
		embeddings, err := s.embedder.EmbedBatch(batch)
		if err != nil {
			return nil, fmt.Errorf("embed batch %d-%d: %w", i, end, err)
		}

		if len(batch) > 1 {
			time.Sleep(50 * time.Millisecond)
		}

		allEmbeddings = append(allEmbeddings, embeddings...)
	}

	return allEmbeddings, nil
}

func countUniqueSources(chunks []Chunk) int {
	seen := make(map[string]bool)
	for _, c := range chunks {
		seen[c.Source] = true
	}
	return len(seen)
}
