package file

import (
	"ai-chat/common/rag"
	"ai-chat/config"
	"ai-chat/utils"
	"context"
	"io"
	"log"
	"mime/multipart"
	"os"
	"path/filepath"
)

// 上传rag相关文件（这里只允许文本文件）
// 其实可以直接将其向量化进行保存，但这边依旧存储到服务器上以便后续可以在服务器上查看历史RAG文件
func UploadRagFile(userEmail string, file *multipart.FileHeader) (string, error) {
	// 校验文件类型和文件名
	if err := utils.ValidateFile(file); err != nil {
		log.Printf("File validation failed: %v", err)
		return "", err
	}

	// 创建用户目录
	userDir := filepath.Join("uploads", userEmail)
	if err := os.MkdirAll(userDir, 0755); err != nil {
		log.Printf("Failed to create user directory %s: %v", userDir, err)
		return "", err
	}

	// 删除用户目录中的所有现有文件及其索引（每个用户只能有一个文件）
	files, err := os.ReadDir(userDir)
	if err == nil {
		for _, f := range files {
			if !f.IsDir() {
				filename := f.Name()
				// 删除该文件对应的 Redis 索引
				if err := rag.DeleteIndex(context.Background(), filename); err != nil {
					log.Printf("Failed to delete index for %s: %v", filename, err)
					// 继续执行，不因为索引删除失败而中断文件上传
				}
			}
		}
	}
	// 删除用户目录中的所有文件
	if err := utils.RemoveAllFilesInDir(userDir); err != nil {
		log.Printf("Failed to clean user directory %s: %v", userDir, err)
		return "", err
	}

	// 生成UUID作为唯一文件名
	uuid := utils.GenerateUUID()

	ext := filepath.Ext(file.Filename)
	filename := uuid + ext
	filePath := filepath.Join(userDir, filename)

	// 打开上传的文件
	src, err := file.Open()
	if err != nil {
		log.Printf("Failed to open uploaded file: %v", err)
		return "", err
	}
	defer src.Close()

	// 创建目标文件
	dst, err := os.Create(filePath)
	if err != nil {
		log.Printf("Failed to create destination file %s: %v", filePath, err)
		return "", err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		log.Printf("Failed to copy file content: %v", err)
		return "", err
	}

	log.Printf("File uploaded successfully: %s", filePath)

	// 创建 RAG 索引器并对文件进行向量化
	indexer, err := rag.NewRAGIndexer(filename, config.GetConfig().RagModelConfig.RagEmbeddingModel)
	if err != nil {
		log.Printf("Failed to create RAG indexer: %v", err)
		// 删除已上传的文件
		os.Remove(filePath)
		return "", err
	}

	// 读取文件内容并创建向量索引
	if err := indexer.IndexFile(context.Background(), filePath); err != nil {
		log.Printf("Failed to index file: %v", err)
		// 删除已上传的文件和索引
		os.Remove(filePath)
		rag.DeleteIndex(context.Background(), filename)
		return "", err
	}

	log.Printf("File indexed successfully: %s", filename)
	return filePath, nil
}
