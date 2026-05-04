package file

import (
	"ai-chat/common/code"
	"ai-chat/common/rag"
	"ai-chat/controller"
	"ai-chat/service/file"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

type (
	UploadFileResponse struct {
		FilePath        string `json:"file_path,omitempty"`
		KnowledgeBaseID string `json:"kbId,omitempty"`
		DocumentID      string `json:"documentId,omitempty"`
		controller.Response
	}

	IndexStatusRequest struct {
		DocumentID string `form:"documentId" binding:"required"`
	}

	IndexStatusResponse struct {
		DocumentID string `json:"documentId,omitempty"`
		Status     string `json:"status,omitempty"`
		Message    string `json:"message,omitempty"`
		UpdatedAt  int64  `json:"updatedAt,omitempty"`
		controller.Response
	}
)

func UploadRagFile(c *gin.Context) {
	res := new(UploadFileResponse)
	uploadedFile, err := c.FormFile("file")
	if err != nil {
		log.Println("FormFile fail ", err)
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidParams))
		return
	}

	userEmail := c.GetString("userEmail")
	if userEmail == "" {
		log.Println("User email not found in context")
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidToken))
		return
	}

	kbID := rag.NormalizeKnowledgeBaseID(c.PostForm("kbId"))
	filePath, indexName, err := file.UploadRagFile(userEmail, kbID, uploadedFile)
	if err != nil {
		log.Println("UploadFile fail", err)
		c.JSON(http.StatusOK, res.CodeOf(code.CodeServerBusy))
		return
	}

	res.Success()
	res.FilePath = filePath
	res.KnowledgeBaseID = kbID
	res.DocumentID = indexName
	c.JSON(http.StatusOK, res)
}

func GetIndexStatus(c *gin.Context) {
	req := new(IndexStatusRequest)
	res := new(IndexStatusResponse)
	if err := c.ShouldBindQuery(req); err != nil {
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidParams))
		return
	}

	status, ok, err := file.GetDocumentIndexStatus(req.DocumentID)
	if err != nil {
		log.Println("GetIndexStatus fail", err)
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidParams))
		return
	}
	if !ok || status == nil {
		c.JSON(http.StatusOK, res.CodeOf(code.CodeRecordNotFound))
		return
	}

	res.Success()
	res.DocumentID = status.DocumentID
	res.Status = string(status.Status)
	res.Message = status.Message
	res.UpdatedAt = status.UpdatedAt
	c.JSON(http.StatusOK, res)
}
