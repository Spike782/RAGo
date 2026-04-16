package file

import (
	"ai-chat/common/code"
	"ai-chat/controller"
	"ai-chat/service/file"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

type (
	UploadFileResponse struct {
		FilePath string `json:"file_path,omitempty"`
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

	filePath, err := file.UploadRagFile(userEmail, uploadedFile)
	if err != nil {
		log.Println("UploadFile fail", err)
		c.JSON(http.StatusOK, res.CodeOf(code.CodeServerBusy))
		return
	}

	res.Success()
	res.FilePath = filePath
	c.JSON(http.StatusOK, res)
}
