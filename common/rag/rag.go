package rag

import (
	"ai-chat/common/redis"
	redisPkg "ai-chat/common/redis"
	"ai-chat/config"
	"context"
	"fmt"
	"os"
	"strings"

	embeddingArk "github.com/cloudwego/eino-ext/components/embedding/ark"
	redisIndexer "github.com/cloudwego/eino-ext/components/indexer/redis"
	redisRetriever "github.com/cloudwego/eino-ext/components/retriever/redis"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
	redisCli "github.com/redis/go-redis/v9"
)

const (
	defaultChunkSize    = 800
	defaultChunkOverlap = 120
	defaultTopK         = 5
)

type RAGIndexer struct {
	embedding    embedding.Embedder
	indexer      *redisIndexer.Indexer
	chunkSize    int
	chunkOverlap int
}

type RAGQuery struct {
	embedding embedding.Embedder
	retriever retriever.Retriever
}

func NewRAGIndexer(filename, embeddingModel string) (*RAGIndexer, error) {
	ctx := context.Background()
	cfg := config.GetConfig()
	apiKey := os.Getenv("OPENAI_API_KEY")
	dimension := cfg.RagModelConfig.RagDimension
	chunkSize, chunkOverlap := normalizeChunkConfig(cfg.RagModelConfig.RagChunkSize, cfg.RagModelConfig.RagChunkOverlap)

	embedConfig := &embeddingArk.EmbeddingConfig{
		BaseURL: cfg.RagModelConfig.RagBaseUrl,
		APIKey:  apiKey,
		Model:   embeddingModel,
	}

	embedder, err := embeddingArk.NewEmbedder(ctx, embedConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create embedder: %w", err)
	}

	if err := redisPkg.InitRedisIndex(ctx, filename, dimension); err != nil {
		return nil, fmt.Errorf("failed to init redis index: %w", err)
	}

	rdb := redisPkg.Rdb
	indexerConfig := &redisIndexer.IndexerConfig{
		Client:    rdb,
		KeyPrefix: redis.GenerateIndexNamePrefix(filename),
		BatchSize: 10,
		DocumentToHashes: func(ctx context.Context, doc *schema.Document) (*redisIndexer.Hashes, error) {
			source := ""
			if s, ok := doc.MetaData["source"].(string); ok {
				source = s
			}

			metadata := source
			if idx, ok := doc.MetaData["chunk_index"]; ok {
				if total, ok2 := doc.MetaData["chunk_total"]; ok2 {
					metadata = fmt.Sprintf("%s [chunk %v/%v]", source, idx, total)
				}
			}

			return &redisIndexer.Hashes{
				Key: fmt.Sprintf("%s:%s", filename, doc.ID),
				Field2Value: map[string]redisIndexer.FieldValue{
					"content":  {Value: doc.Content, EmbedKey: "vector"},
					"metadata": {Value: metadata},
				},
			}, nil
		},
	}
	indexerConfig.Embedding = embedder

	idx, err := redisIndexer.NewIndexer(ctx, indexerConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create indexer: %w", err)
	}

	return &RAGIndexer{
		embedding:    embedder,
		indexer:      idx,
		chunkSize:    chunkSize,
		chunkOverlap: chunkOverlap,
	}, nil
}

func (r *RAGIndexer) IndexFile(ctx context.Context, filePath string) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	text := strings.TrimSpace(string(content))
	if text == "" {
		return fmt.Errorf("file is empty")
	}

	chunks := splitTextIntoChunks(text, r.chunkSize, r.chunkOverlap)
	if len(chunks) == 0 {
		chunks = []string{text}
	}

	docs := make([]*schema.Document, 0, len(chunks))
	for i, chunk := range chunks {
		docs = append(docs, &schema.Document{
			ID:      fmt.Sprintf("chunk_%d", i+1),
			Content: chunk,
			MetaData: map[string]any{
				"source":      filePath,
				"chunk_index": i + 1,
				"chunk_total": len(chunks),
			},
		})
	}

	if _, err = r.indexer.Store(ctx, docs); err != nil {
		return fmt.Errorf("failed to store document chunks: %w", err)
	}
	return nil
}

func DeleteIndex(ctx context.Context, filename string) error {
	if err := redisPkg.DeleteRedisIndex(ctx, filename); err != nil {
		return fmt.Errorf("failed to delete redis index: %w", err)
	}
	return nil
}

func NewRAGQuery(ctx context.Context, userEmail string) (*RAGQuery, error) {
	cfg := config.GetConfig()
	apiKey := os.Getenv("OPENAI_API_KEY")

	embedConfig := &embeddingArk.EmbeddingConfig{
		BaseURL: cfg.RagModelConfig.RagBaseUrl,
		APIKey:  apiKey,
		Model:   cfg.RagModelConfig.RagEmbeddingModel,
	}
	embedder, err := embeddingArk.NewEmbedder(ctx, embedConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create embedder: %w", err)
	}

	userDir := fmt.Sprintf("uploads/%s", userEmail)
	files, err := os.ReadDir(userDir)
	if err != nil || len(files) == 0 {
		return nil, fmt.Errorf("no uploaded file found for user %s", userEmail)
	}

	var filename string
	for _, f := range files {
		if !f.IsDir() {
			filename = f.Name()
			break
		}
	}
	if filename == "" {
		return nil, fmt.Errorf("no valid file found for user %s", userEmail)
	}

	topK := cfg.RagModelConfig.RagTopK
	if topK <= 0 {
		topK = defaultTopK
	}

	rdb := redisPkg.Rdb
	indexName := redis.GenerateIndexName(filename)
	retrieverConfig := &redisRetriever.RetrieverConfig{
		Client:       rdb,
		Index:        indexName,
		Dialect:      2,
		ReturnFields: []string{"content", "metadata", "distance"},
		TopK:         topK,
		VectorField:  "vector",
		DocumentConverter: func(ctx context.Context, doc redisCli.Document) (*schema.Document, error) {
			resp := &schema.Document{
				ID:       doc.ID,
				Content:  "",
				MetaData: map[string]any{},
			}
			for field, val := range doc.Fields {
				if field == "content" {
					resp.Content = val
				} else {
					resp.MetaData[field] = val
				}
			}
			return resp, nil
		},
	}
	retrieverConfig.Embedding = embedder

	rtr, err := redisRetriever.NewRetriever(ctx, retrieverConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create retriever: %w", err)
	}

	return &RAGQuery{
		embedding: embedder,
		retriever: rtr,
	}, nil
}

func (r *RAGQuery) RetrieveDocuments(ctx context.Context, query string) ([]*schema.Document, error) {
	docs, err := r.retriever.Retrieve(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve documents: %w", err)
	}
	return docs, nil
}

func BuildRAGPrompt(query string, docs []*schema.Document) string {
	if len(docs) == 0 {
		return query
	}

	var builder strings.Builder
	for i, doc := range docs {
		builder.WriteString(fmt.Sprintf("[Document %d]: %s\n\n", i+1, doc.Content))
	}

	return fmt.Sprintf(`Answer the user's question based on the following reference content.
If the references are insufficient, explicitly say you cannot find enough information.

References:
%s
User question: %s

Please provide a concise and accurate answer.`, builder.String(), query)
}

func normalizeChunkConfig(size, overlap int) (int, int) {
	if size <= 0 {
		size = defaultChunkSize
	}
	if overlap < 0 {
		overlap = 0
	}
	if overlap >= size {
		overlap = size / 4
	}
	return size, overlap
}

func splitTextIntoChunks(text string, chunkSize, chunkOverlap int) []string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) == 0 {
		return nil
	}

	chunkSize, chunkOverlap = normalizeChunkConfig(chunkSize, chunkOverlap)
	step := chunkSize - chunkOverlap
	if step <= 0 {
		step = chunkSize
	}

	chunks := make([]string, 0, (len(runes)/step)+1)
	start := 0

	for start < len(runes) {
		end := start + chunkSize
		if end > len(runes) {
			end = len(runes)
		}

		split := findSplitPosition(runes, start, end)
		if split <= start {
			split = end
		}

		chunk := strings.TrimSpace(string(runes[start:split]))
		if chunk != "" {
			chunks = append(chunks, chunk)
		}

		if split >= len(runes) {
			break
		}

		next := split - chunkOverlap
		if next <= start {
			next = start + step
		}
		start = next
	}

	return chunks
}

func findSplitPosition(runes []rune, start, end int) int {
	if end >= len(runes) {
		return end
	}

	const window = 120
	min := start
	if end-window > min {
		min = end - window
	}

	for i := end - 1; i >= min; i-- {
		if isBoundaryRune(runes[i]) {
			return i + 1
		}
	}
	return end
}

func isBoundaryRune(r rune) bool {
	switch r {
	case '\n', '\r', '\t', ' ', '。', '！', '？', '；', '，', '、', '.', ',', '!', '?', ';', ':':
		return true
	default:
		return false
	}
}
