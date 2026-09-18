package rag

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

type VectorEntry struct {
	ID        string    `json:"id"`
	Content   string    `json:"content"`
	Source    string    `json:"source"`
	LineFrom  int       `json:"line_from"`
	LineTo    int       `json:"line_to"`
	ChunkIdx  int       `json:"chunk_idx"`
	Embedding []float64 `json:"-"`
}

type VectorStore struct {
	db *sql.DB
}

func NewVectorStore(dbPath string) (*VectorStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open vector store: %w", err)
	}

	store := &VectorStore{db: db}
	if err := store.setupPRAGMAs(); err != nil {
		db.Close()
		return nil, fmt.Errorf("setup pragmas: %w", err)
	}
	if err := store.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	return store, nil
}

// initSchema and setupPRAGMAs run only from NewVectorStore during process
// startup, before any request exists, so there is no caller context to thread.
func (vs *VectorStore) initSchema() error {
	_, err := vs.db.ExecContext(context.Background(), `
		CREATE TABLE IF NOT EXISTS chunks (
			id TEXT PRIMARY KEY,
			content TEXT NOT NULL,
			source TEXT NOT NULL,
			line_from INTEGER DEFAULT 0,
			line_to INTEGER DEFAULT 0,
			chunk_idx INTEGER DEFAULT 0,
			embedding TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_chunks_source ON chunks(source);
	`)
	return err
}

func (vs *VectorStore) setupPRAGMAs() error {
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA foreign_keys=ON",
	}
	for _, p := range pragmas {
		if _, err := vs.db.ExecContext(context.Background(), p); err != nil {
			return err
		}
	}
	return nil
}

func (vs *VectorStore) AddChunk(ctx context.Context, chunk Chunk, embedding []float64) error {
	embJSON, err := json.Marshal(embedding)
	if err != nil {
		return fmt.Errorf("marshal embedding: %w", err)
	}

	_, err = vs.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO chunks (id, content, source, line_from, line_to, chunk_idx, embedding)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		chunk.ID, chunk.Content, chunk.Source, chunk.LineFrom, chunk.LineTo, chunk.ChunkIdx, embJSON,
	)
	return err
}

func (vs *VectorStore) AddChunks(ctx context.Context, chunks []Chunk, embeddings [][]float64) error {
	tx, err := vs.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx,
		`INSERT OR REPLACE INTO chunks (id, content, source, line_from, line_to, chunk_idx, embedding)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
	)
	if err != nil {
		return fmt.Errorf("prepare statement: %w", err)
	}
	defer stmt.Close()

	for i, chunk := range chunks {
		embJSON, err := json.Marshal(embeddings[i])
		if err != nil {
			return fmt.Errorf("marshal embedding %d: %w", i, err)
		}
		_, err = stmt.ExecContext(ctx, chunk.ID, chunk.Content, chunk.Source, chunk.LineFrom, chunk.LineTo, chunk.ChunkIdx, embJSON)
		if err != nil {
			return fmt.Errorf("exec insert %d: %w", i, err)
		}
	}

	return tx.Commit()
}

func (vs *VectorStore) Search(ctx context.Context, queryEmbedding []float64, topK int) ([]VectorEntry, error) {
	rows, err := vs.db.QueryContext(ctx, `SELECT id, content, source, line_from, line_to, chunk_idx, embedding FROM chunks`)
	if err != nil {
		return nil, fmt.Errorf("query chunks: %w", err)
	}
	defer rows.Close()

	var results []scoredResult

	for rows.Next() {
		var id, content, source, embJSON string
		var lineFrom, lineTo, chunkIdx int

		if err := rows.Scan(&id, &content, &source, &lineFrom, &lineTo, &chunkIdx, &embJSON); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}

		var embedding []float64
		if err := json.Unmarshal([]byte(embJSON), &embedding); err != nil {
			return nil, fmt.Errorf("unmarshal embedding: %w", err)
		}

		score := cosineSimilarity(queryEmbedding, embedding)
		results = append(results, scoredResult{
			entry: VectorEntry{
				ID:        id,
				Content:   content,
				Source:    source,
				LineFrom:  lineFrom,
				LineTo:    lineTo,
				ChunkIdx:  chunkIdx,
				Embedding: embedding,
			},
			score: score,
		})
	}

	sortScored(results)

	if topK > len(results) {
		topK = len(results)
	}

	var top []VectorEntry
	for i := 0; i < topK; i++ {
		top = append(top, results[i].entry)
	}

	return top, nil
}

func (vs *VectorStore) Count(ctx context.Context) (int, error) {
	var count int
	err := vs.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM chunks").Scan(&count)
	return count, err
}

func (vs *VectorStore) Clear(ctx context.Context) error {
	_, err := vs.db.ExecContext(ctx, "DELETE FROM chunks")
	return err
}

// ReplaceDirectory atomically swaps the stored chunks for one directory
// scope. Callers prepare chunks+embeddings first, so a failed preparation
// never reaches this method and old results stay searchable. Only rows whose
// source is being replaced or that live under dirScope are deleted —
// unrelated sources (teach/, other directories) are kept. Stale rows for
// files deleted from the directory since the last index match the scope
// prefix and are removed too.
func (vs *VectorStore) ReplaceDirectory(ctx context.Context, dirScope string, chunks []Chunk, embeddings [][]float64) error {
	scope := filepath.Clean(dirScope)
	fresh := make(map[string]bool, len(chunks))
	for _, c := range chunks {
		fresh[c.Source] = true
	}

	tx, err := vs.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT source FROM chunks`)
	if err != nil {
		return fmt.Errorf("list sources: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var doomed []string
	for rows.Next() {
		var src string
		if err := rows.Scan(&src); err != nil {
			return fmt.Errorf("scan source: %w", err)
		}
		if fresh[src] || src == scope || strings.HasPrefix(src, scope+string(os.PathSeparator)) {
			doomed = append(doomed, src)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate sources: %w", err)
	}

	if len(doomed) > 0 {
		placeholders := make([]string, len(doomed))
		args := make([]any, len(doomed))
		for i, src := range doomed {
			placeholders[i] = fmt.Sprintf("$%d", i+1)
			args[i] = src
		}
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM chunks WHERE source IN (`+strings.Join(placeholders, ",")+`)`, args...,
		); err != nil {
			return fmt.Errorf("delete scoped chunks: %w", err)
		}
	}

	if len(chunks) > 0 {
		stmt, err := tx.PrepareContext(ctx,
			`INSERT OR REPLACE INTO chunks (id, content, source, line_from, line_to, chunk_idx, embedding)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		)
		if err != nil {
			return fmt.Errorf("prepare statement: %w", err)
		}
		defer func() { _ = stmt.Close() }()

		for i, chunk := range chunks {
			embJSON, err := json.Marshal(embeddings[i])
			if err != nil {
				return fmt.Errorf("marshal embedding %d: %w", i, err)
			}
			_, err = stmt.ExecContext(ctx, chunk.ID, chunk.Content, chunk.Source, chunk.LineFrom, chunk.LineTo, chunk.ChunkIdx, embJSON)
			if err != nil {
				return fmt.Errorf("exec insert %d: %w", i, err)
			}
		}
	}

	return tx.Commit()
}

func (vs *VectorStore) Close() error {
	return vs.db.Close()
}

func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

type scoredResult struct {
	entry VectorEntry
	score float64
}

func sortScored(results []scoredResult) {
	for i := 1; i < len(results); i++ {
		key := results[i]
		j := i - 1
		for j >= 0 && results[j].score < key.score {
			results[j+1] = results[j]
			j--
		}
		results[j+1] = key
	}
}
