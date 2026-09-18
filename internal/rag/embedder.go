package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"time"
)

type Embedder interface {
	Embed(ctx context.Context, text string) ([]float64, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float64, error)
	Dimension() int
}

type OpenAIEmbedder struct {
	apiKey    string
	baseURL   string
	model     string
	client    *http.Client
	dimension int
}

type embedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type embedResponse struct {
	Data []embedData `json:"data"`
}

type embedData struct {
	Embedding []float64 `json:"embedding"`
}

func NewOpenAIEmbedder(apiKey, baseURL, model string) *OpenAIEmbedder {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if model == "" {
		model = "text-embedding-3-small"
	}
	return &OpenAIEmbedder{
		apiKey:    apiKey,
		baseURL:   baseURL,
		model:     model,
		client:    &http.Client{Timeout: 60 * time.Second},
		dimension: 1536,
	}
}

func (e *OpenAIEmbedder) Embed(ctx context.Context, text string) ([]float64, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	reqBody, err := json.Marshal(embedRequest{Model: e.model, Input: text})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", e.baseURL+"/embeddings", bytes.NewReader(reqBody)) //nolint:gosec // G704: baseURL is operator-configured provider endpoint, never request input
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	var result embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(result.Data) == 0 {
		return nil, fmt.Errorf("empty response data")
	}

	return result.Data[0].Embedding, nil
}

func (e *OpenAIEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float64, error) {
	results := make([][]float64, len(texts))
	for i, text := range texts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		emb, err := e.Embed(ctx, text)
		if err != nil {
			return nil, fmt.Errorf("embed batch item %d: %w", i, err)
		}
		results[i] = emb
	}
	return results, nil
}

func (e *OpenAIEmbedder) Dimension() int {
	return e.dimension
}

type HashEmbedder struct {
	dimension int
	rng       *rand.Rand
}

func NewHashEmbedder(dimensions int) *HashEmbedder {
	if dimensions <= 0 {
		dimensions = 384
	}
	return &HashEmbedder{
		dimension: dimensions,
		rng:       rand.New(rand.NewSource(42)),
	}
}

func (h *HashEmbedder) Embed(ctx context.Context, text string) ([]float64, error) {
	vectors := make([][]float64, 1)
	embeddings, err := h.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	vectors[0] = embeddings[0]
	return vectors[0], nil
}

func (h *HashEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float64, error) {
	results := make([][]float64, len(texts))
	for i, text := range texts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		results[i] = hashEmbed(text, h.dimension, h.rng)
	}
	return results, nil
}

func (h *HashEmbedder) Dimension() int {
	return h.dimension
}

func hashEmbed(text string, dim int, rng *rand.Rand) []float64 {
	result := make([]float64, dim)
	for i := 0; i < dim; i++ {
		h := fnvHash(text, i)
		result[i] = (float64(h)/4294967295.0)*2 - 1
	}
	// L2 normalize
	norm := 0.0
	for _, v := range result {
		norm += v * v
	}
	norm = float64(int(norm*10000)) / 10000
	if norm > 0 {
		for i := range result {
			result[i] /= norm
		}
	}
	return result
}

func fnvHash(s string, seed int) uint32 {
	h := uint32(2166136261 ^ uint32(seed))
	for i := range s {
		h ^= uint32(s[i])
		h *= 16777619
		h ^= h >> 16
		h *= 16777619
		h ^= h >> 13
		h *= 16777619
	}
	return h
}
