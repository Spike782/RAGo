package rag

import (
	"ai-chat/config"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/schema"
)

const defaultQdrantTimeoutSeconds = 15

type QdrantStore struct {
	baseURL string
	apiKey  string
	timeout time.Duration
	prefix  string
}

var chunkIDRegexp = regexp.MustCompile(`^chunk_(\d+)$`)

func NewQdrantStore() *QdrantStore {
	cfg := config.GetConfig().RagModelConfig
	apiKey := strings.TrimSpace(cfg.RagQdrantAPIKey)
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("QDRANT_API_KEY"))
	}

	timeoutSec := cfg.RagQdrantTimeoutS
	if timeoutSec <= 0 {
		timeoutSec = defaultQdrantTimeoutSeconds
	}

	prefix := strings.TrimSpace(cfg.RagQdrantPrefix)
	if prefix == "" {
		prefix = "rag_docs_"
	}

	return &QdrantStore{
		baseURL: strings.TrimRight(strings.TrimSpace(cfg.RagQdrantURL), "/"),
		apiKey:  apiKey,
		timeout: time.Duration(timeoutSec) * time.Second,
		prefix:  prefix,
	}
}

func (s *QdrantStore) Name() string {
	return "qdrant"
}

func (s *QdrantStore) EnsureIndex(ctx context.Context, filename string, dimension int) error {
	collection := s.collectionName(filename)
	if collection == "" {
		return fmt.Errorf("invalid qdrant collection name")
	}

	exists, err := s.collectionExists(ctx, collection)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	body := map[string]any{
		"vectors": map[string]any{
			"size":     dimension,
			"distance": "Cosine",
		},
	}
	return s.requestJSON(ctx, http.MethodPut, "/collections/"+collection, body, nil)
}

func (s *QdrantStore) IndexDocuments(ctx context.Context, filename string, docs []*schema.Document, embedder embedding.Embedder) error {
	if len(docs) == 0 {
		return nil
	}
	texts := make([]string, 0, len(docs))
	for _, d := range docs {
		texts = append(texts, d.Content)
	}

	embeddings, err := embedder.EmbedStrings(ctx, texts)
	if err != nil {
		return fmt.Errorf("embed documents failed: %w", err)
	}
	if len(embeddings) != len(docs) {
		return fmt.Errorf("embedding/doc count mismatch: %d/%d", len(embeddings), len(docs))
	}

	points := make([]map[string]any, 0, len(docs))
	for i, doc := range docs {
		points = append(points, map[string]any{
			"id":     qdrantPointID(doc.ID, i+1),
			"vector": embeddings[i],
			"payload": map[string]any{
				"content":  doc.Content,
				"metadata": doc.MetaData,
			},
		})
	}

	body := map[string]any{
		"points": points,
	}
	path := fmt.Sprintf("/collections/%s/points?wait=true", s.collectionName(filename))
	return s.requestJSON(ctx, http.MethodPut, path, body, nil)
}

func (s *QdrantStore) Retrieve(ctx context.Context, filename, query string, topK int, embedder embedding.Embedder) ([]*schema.Document, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	vectors, err := embedder.EmbedStrings(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("embed query failed: %w", err)
	}
	if len(vectors) == 0 {
		return nil, nil
	}

	return s.RetrieveByVector(ctx, filename, vectors[0], topK)
}

func (s *QdrantStore) RetrieveByVector(ctx context.Context, filename string, vector []float64, topK int) ([]*schema.Document, error) {
	if len(vector) == 0 {
		return nil, nil
	}

	body := map[string]any{
		"vector":       vector,
		"limit":        topK,
		"with_payload": true,
	}

	var resp struct {
		Result []struct {
			ID      any            `json:"id"`
			Score   float64        `json:"score"`
			Payload map[string]any `json:"payload"`
		} `json:"result"`
	}
	path := fmt.Sprintf("/collections/%s/points/search", s.collectionName(filename))
	if err := s.requestJSON(ctx, http.MethodPost, path, body, &resp); err != nil {
		return nil, err
	}

	docs := make([]*schema.Document, 0, len(resp.Result))
	for _, item := range resp.Result {
		content, _ := item.Payload["content"].(string)
		meta := map[string]any{
			"score": item.Score,
		}
		if payloadMeta, ok := item.Payload["metadata"].(map[string]any); ok {
			for k, v := range payloadMeta {
				meta[k] = v
			}
		}

		docs = append(docs, &schema.Document{
			ID:       fmt.Sprint(item.ID),
			Content:  content,
			MetaData: meta,
		})
	}
	return docs, nil
}

func (s *QdrantStore) DeleteIndex(ctx context.Context, filename string) error {
	return s.requestJSON(ctx, http.MethodDelete, "/collections/"+s.collectionName(filename), nil, nil)
}

func (s *QdrantStore) collectionExists(ctx context.Context, collection string) (bool, error) {
	var out map[string]any
	err := s.requestJSON(ctx, http.MethodGet, "/collections/"+collection, nil, &out)
	if err == nil {
		return true, nil
	}
	if strings.Contains(strings.ToLower(err.Error()), "404") || strings.Contains(strings.ToLower(err.Error()), "not found") {
		return false, nil
	}
	return false, err
}

func (s *QdrantStore) requestJSON(ctx context.Context, method, path string, body any, out any) error {
	if s.baseURL == "" {
		return fmt.Errorf("qdrant url is empty")
	}
	reqCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	var bodyReader *strings.Reader
	if body == nil {
		bodyReader = strings.NewReader("")
	} else {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request failed: %w", err)
		}
		bodyReader = strings.NewReader(string(raw))
	}

	req, err := http.NewRequestWithContext(reqCtx, method, s.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("create request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if s.apiKey != "" {
		req.Header.Set("api-key", s.apiKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("qdrant request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		msg := strings.TrimSpace(string(raw))
		if msg == "" {
			return fmt.Errorf("qdrant status %d", resp.StatusCode)
		}
		return fmt.Errorf("qdrant status %d: %s", resp.StatusCode, msg)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode qdrant response failed: %w", err)
	}
	return nil
}

func qdrantPointID(docID string, fallback int) any {
	id := strings.TrimSpace(docID)
	if id == "" {
		if fallback < 1 {
			return uint64(1)
		}
		return uint64(fallback)
	}

	if n, err := strconv.ParseUint(id, 10, 64); err == nil && n > 0 {
		return n
	}
	if m := chunkIDRegexp.FindStringSubmatch(id); len(m) == 2 {
		if n, err := strconv.ParseUint(m[1], 10, 64); err == nil && n > 0 {
			return n
		}
	}

	// Qdrant point id must be uint64 or UUID; use deterministic uint64 hash for generic ids.
	h := fnv.New64a()
	_, _ = h.Write([]byte(id))
	n := h.Sum64()
	if n == 0 {
		n = 1
	}
	return n
}

func (s *QdrantStore) collectionName(filename string) string {
	name := sanitizeIdentifier(filename)
	if name == "" {
		name = "default"
	}
	return s.prefix + name
}

func sanitizeIdentifier(in string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(in) {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
		case r == '_', r == '-':
			b.WriteRune('_')
		}
	}
	return strings.Trim(b.String(), "_")
}
