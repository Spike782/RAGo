package redis

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	redisCli "github.com/redis/go-redis/v9"
)

const ragIndexStatusTTL = 24 * time.Hour

type RAGIndexStatus string

const (
	RAGIndexStatusPending  RAGIndexStatus = "pending"
	RAGIndexStatusIndexing RAGIndexStatus = "indexing"
	RAGIndexStatusReady    RAGIndexStatus = "ready"
	RAGIndexStatusFailed   RAGIndexStatus = "failed"
)

type DocumentIndexStatus struct {
	DocumentID string         `json:"documentId"`
	Status     RAGIndexStatus `json:"status"`
	Message    string         `json:"message,omitempty"`
	UpdatedAt  int64          `json:"updatedAt"`
}

func ragIndexStatusKey(documentID string) string {
	return fmt.Sprintf("cache:rag_index_status:%s", strings.TrimSpace(documentID))
}

func SetDocumentIndexStatus(documentID string, status RAGIndexStatus, message string) error {
	if Rdb == nil {
		return nil
	}
	documentID = strings.TrimSpace(documentID)
	if documentID == "" {
		return nil
	}
	raw, err := json.Marshal(DocumentIndexStatus{
		DocumentID: documentID,
		Status:     status,
		Message:    strings.TrimSpace(message),
		UpdatedAt:  time.Now().Unix(),
	})
	if err != nil {
		return err
	}
	return Rdb.Set(ctx, ragIndexStatusKey(documentID), raw, ragIndexStatusTTL).Err()
}

func GetDocumentIndexStatus(documentID string) (*DocumentIndexStatus, bool, error) {
	if Rdb == nil {
		return nil, false, nil
	}
	documentID = strings.TrimSpace(documentID)
	if documentID == "" {
		return nil, false, nil
	}
	raw, err := Rdb.Get(ctx, ragIndexStatusKey(documentID)).Result()
	if err != nil {
		if err == redisCli.Nil {
			return nil, false, nil
		}
		return nil, false, err
	}

	var out DocumentIndexStatus
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, false, err
	}
	return &out, true, nil
}

