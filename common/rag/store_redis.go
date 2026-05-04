package rag

import (
	"ai-chat/common/redis"
	"context"
	"fmt"

	redisIndexer "github.com/cloudwego/eino-ext/components/indexer/redis"
	redisRetriever "github.com/cloudwego/eino-ext/components/retriever/redis"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/schema"
	redisCli "github.com/redis/go-redis/v9"
)

type RedisVectorStore struct {
}

func NewRedisVectorStore() *RedisVectorStore {
	return &RedisVectorStore{}
}

func (s *RedisVectorStore) Name() string {
	return "redis"
}

func (s *RedisVectorStore) EnsureIndex(ctx context.Context, filename string, dimension int) error {
	return redis.InitRedisIndex(ctx, filename, dimension)
}

func (s *RedisVectorStore) IndexDocuments(ctx context.Context, filename string, docs []*schema.Document, embedder embedding.Embedder) error {
	indexerConfig := &redisIndexer.IndexerConfig{
		Client:    redis.Rdb,
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
		return fmt.Errorf("failed to create redis indexer: %w", err)
	}
	if _, err = idx.Store(ctx, docs); err != nil {
		return fmt.Errorf("failed to store redis documents: %w", err)
	}
	return nil
}

func (s *RedisVectorStore) Retrieve(ctx context.Context, filename, query string, topK int, embedder embedding.Embedder) ([]*schema.Document, error) {
	retrieverConfig := &redisRetriever.RetrieverConfig{
		Client:       redis.Rdb,
		Index:        redis.GenerateIndexName(filename),
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
		return nil, fmt.Errorf("failed to create redis retriever: %w", err)
	}
	docs, err := rtr.Retrieve(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve redis documents: %w", err)
	}
	return docs, nil
}

func (s *RedisVectorStore) DeleteIndex(ctx context.Context, filename string) error {
	return redis.DeleteRedisIndex(ctx, filename)
}
