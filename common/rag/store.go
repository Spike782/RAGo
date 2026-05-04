package rag

import (
	"ai-chat/config"
	"context"
	"strings"

	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/schema"
)

type VectorStore interface {
	Name() string
	EnsureIndex(ctx context.Context, filename string, dimension int) error
	IndexDocuments(ctx context.Context, filename string, docs []*schema.Document, embedder embedding.Embedder) error
	Retrieve(ctx context.Context, filename, query string, topK int, embedder embedding.Embedder) ([]*schema.Document, error)
	DeleteIndex(ctx context.Context, filename string) error
}

func NewVectorStore() VectorStore {
	cfg := config.GetConfig().RagModelConfig
	switch strings.ToLower(strings.TrimSpace(cfg.RagVectorStore)) {
	case "qdrant":
		return NewQdrantStore()
	default:
		return NewRedisVectorStore()
	}
}
