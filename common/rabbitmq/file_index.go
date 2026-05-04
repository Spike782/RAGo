package rabbitmq

import (
	"ai-chat/common/rag"
	myredis "ai-chat/common/redis"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/streadway/amqp"
)

type FileIndexMQParam struct {
	UserEmail  string `json:"user_email"`
	KBID       string `json:"kb_id"`
	FilePath   string `json:"file_path"`
	DocumentID string `json:"document_id"`
}

func GenerateFileIndexMQParam(userEmail, kbID, filePath, documentID string) []byte {
	param := FileIndexMQParam{
		UserEmail:  strings.TrimSpace(userEmail),
		KBID:       rag.NormalizeKnowledgeBaseID(kbID),
		FilePath:   strings.TrimSpace(filePath),
		DocumentID: strings.TrimSpace(documentID),
	}
	data, _ := json.Marshal(param)
	return data
}

func MQFileIndex(msg *amqp.Delivery) error {
	var param FileIndexMQParam
	if err := json.Unmarshal(msg.Body, &param); err != nil {
		return err
	}

	if strings.TrimSpace(param.DocumentID) == "" || strings.TrimSpace(param.FilePath) == "" {
		return fmt.Errorf("invalid file index message")
	}

	_ = myredis.SetDocumentIndexStatus(param.DocumentID, myredis.RAGIndexStatusIndexing, "")
	if _, err := rag.IndexDocumentFile(context.Background(), param.KBID, param.FilePath); err != nil {
		_ = myredis.SetDocumentIndexStatus(param.DocumentID, myredis.RAGIndexStatusFailed, err.Error())
		return err
	}

	_ = myredis.SetDocumentIndexStatus(param.DocumentID, myredis.RAGIndexStatusReady, "")
	return nil
}

