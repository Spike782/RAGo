package rag

import (
	"ai-chat/config"
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// IndexDocumentFile indexes one local file into vector store by computed document index name.
func IndexDocumentFile(ctx context.Context, kbID, filePath string) (string, error) {
	kbID = NormalizeKnowledgeBaseID(kbID)
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return "", fmt.Errorf("file path is empty")
	}

	fileName := filepath.Base(filePath)
	indexName := BuildDocumentIndexName(kbID, fileName)
	indexer, err := NewRAGIndexer(indexName, config.GetConfig().RagModelConfig.RagEmbeddingModel)
	if err != nil {
		return "", err
	}
	if err := indexer.IndexFile(ctx, filePath); err != nil {
		return "", err
	}
	return indexName, nil
}

