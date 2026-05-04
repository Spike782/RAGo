package file

import (
	"ai-chat/common/rabbitmq"
	"ai-chat/common/rag"
	myredis "ai-chat/common/redis"
	"ai-chat/utils"
	"errors"
	"io"
	"log"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
)

// UploadRagFile uploads one document into a knowledge base and enqueues async indexing.
func UploadRagFile(userEmail, kbID string, file *multipart.FileHeader) (string, string, error) {
	if err := utils.ValidateFile(file); err != nil {
		log.Printf("File validation failed: %v", err)
		return "", "", err
	}

	kbID = rag.NormalizeKnowledgeBaseID(kbID)
	userDir := rag.UserKnowledgeBaseDir(userEmail, kbID)
	if err := os.MkdirAll(userDir, 0755); err != nil {
		log.Printf("Failed to create user directory %s: %v", userDir, err)
		return "", "", err
	}

	ext := filepath.Ext(file.Filename)
	fileName := utils.GenerateUUID() + ext
	filePath := filepath.Join(userDir, fileName)
	indexName := rag.BuildDocumentIndexName(kbID, fileName)

	src, err := file.Open()
	if err != nil {
		log.Printf("Failed to open uploaded file: %v", err)
		return "", "", err
	}
	defer src.Close()

	dst, err := os.Create(filePath)
	if err != nil {
		log.Printf("Failed to create destination file %s: %v", filePath, err)
		return "", "", err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		log.Printf("Failed to copy file content: %v", err)
		return "", "", err
	}

	if rabbitmq.RMQFileIdx == nil {
		log.Printf("RAG index queue is not initialized")
		_ = os.Remove(filePath)
		return "", "", errors.New("rag index queue is not initialized")
	}

	_ = myredis.SetDocumentIndexStatus(indexName, myredis.RAGIndexStatusPending, "")
	msg := rabbitmq.GenerateFileIndexMQParam(userEmail, kbID, filePath, indexName)
	if err := rabbitmq.RMQFileIdx.Publish(msg); err != nil {
		_ = myredis.SetDocumentIndexStatus(indexName, myredis.RAGIndexStatusFailed, err.Error())
		log.Printf("Failed to enqueue index task: %v", err)
		return "", "", err
	}

	log.Printf("File uploaded and queued for indexing: kb=%s file=%s index=%s", kbID, filePath, indexName)
	return filePath, indexName, nil
}

func GetDocumentIndexStatus(documentID string) (*myredis.DocumentIndexStatus, bool, error) {
	documentID = strings.TrimSpace(documentID)
	if documentID == "" {
		return nil, false, errors.New("documentId is empty")
	}
	return myredis.GetDocumentIndexStatus(documentID)
}
