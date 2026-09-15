package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]
	apiURL := getEnv("RAG_API_URL", "http://localhost:8080")
	token := os.Getenv("RAG_API_TOKEN")

	switch command {
	case "stats":
		handleStats(apiURL, token)
	case "teach":
		handleTeach(apiURL, token, os.Args[2:])
	case "search":
		handleSearch(apiURL, token, os.Args[2:])
	case "index":
		handleIndex(apiURL, token, os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

// authorizedRequest issues an HTTP request with Bearer auth when a token is
// configured. The RAG API routes sit behind RequireAPIAuth; without the header
// the CLI always gets 401 "api token invalid".
func authorizedRequest(apiURL, token, method, path, contentType string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, apiURL+path, body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return http.DefaultClient.Do(req)
}

func handleTeach(apiURL, token string, args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: rag teach <topic> [file1 file2 ...]")
		os.Exit(1)
	}

	topic := args[0]
	files := args[1:]

	if len(files) == 0 {
		// Read from stdin
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to read stdin: %v\n", err)
			os.Exit(1)
		}
		if len(data) == 0 {
			fmt.Fprintln(os.Stderr, "no content to teach (pipe content via stdin)")
			os.Exit(1)
		}
		reqBody, _ := json.Marshal(map[string]any{
			"name":    topic,
			"content": string(data),
		})
		resp, err := authorizedRequest(apiURL, token, http.MethodPost, "/api/rag/teach", "application/json", bytes.NewReader(reqBody))
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to teach: %v\n", err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		printResponse(resp)
		return
	}

	// Read files and combine content
	var combined strings.Builder
	for _, fp := range files {
		data, err := os.ReadFile(fp)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to read %s: %v\n", fp, err)
			continue
		}
		combined.WriteString(fmt.Sprintf("=== %s ===\n", fp))
		combined.WriteString(string(data))
		combined.WriteString("\n\n")
	}

	if combined.Len() == 0 {
		fmt.Fprintln(os.Stderr, "no files found or readable")
		os.Exit(1)
	}

	reqBody, _ := json.Marshal(map[string]any{
		"name":    topic,
		"content": combined.String(),
	})
	resp, err := authorizedRequest(apiURL, token, http.MethodPost, "/api/rag/teach", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to teach: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	printResponse(resp)
}

func handleSearch(apiURL, token string, args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: rag search <query> [top_k]")
		os.Exit(1)
	}

	query := args[0]
	topK := 5
	if len(args) > 1 {
		fmt.Sscanf(args[1], "%d", &topK)
	}

	reqBody, _ := json.Marshal(map[string]any{
		"query": query,
		"top_k": topK,
	})
	resp, err := authorizedRequest(apiURL, token, http.MethodPost, "/api/rag/search", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to search: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)

	chunks, _ := result["chunks"].([]any)
	for i, c := range chunks {
		chunk := c.(map[string]any)
		score := 0.0
		if scores, ok := result["scores"].([]any); ok && i < len(scores) {
			score = scores[i].(float64)
		}
		fmt.Printf("\n--- Result %d (score: %.3f) ---\n", i+1, score)
		fmt.Printf("Source: %s\n", chunk["source"])
		fmt.Printf("Content: %s\n", truncate(chunk["content"].(string), 500))
	}
}

func handleStats(apiURL, token string) {
	resp, err := authorizedRequest(apiURL, token, http.MethodGet, "/api/rag/stats", "", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get stats: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	printResponse(resp)
}

func handleIndex(apiURL, token string, args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: rag index <directory>")
		os.Exit(1)
	}

	reqBody, _ := json.Marshal(map[string]any{
		"directory": args[0],
	})
	resp, err := authorizedRequest(apiURL, token, http.MethodPost, "/api/rag/index", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to index: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	printResponse(resp)
}

func printResponse(resp *http.Response) {
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("[%d] %s\n", resp.StatusCode, string(body))
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func printUsage() {
	fmt.Println(`rag - CLI for codebase RAG

Usage:
  rag teach <topic> [file1 file2 ...]  Teach the RAG about a topic
  rag search <query> [top_k]           Search the RAG
  rag stats                            Show chunk count
  rag index <directory>                Index a directory

Examples:
  rag teach "booking policies" docs/business-rules/booking.md
  echo "Our policy is..." | rag teach "company policies"
  rag search "how are invoices generated"
  rag index ./docs`)
}
