package session

import (
	"ai-chat/common/code"
	"ai-chat/common/rag"
	"ai-chat/controller"
	"ai-chat/model"
	"ai-chat/service/session"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

type (
	GetUserSessionsResponse struct {
		controller.Response
		Sessions []model.SessionInfo `json:"sessions,omitempty"`
	}

	CreateSessionAndSendMessageRequest struct {
		UserQuestion string `json:"question" binding:"required"`
		ModelType    string `json:"modelType" binding:"required"`
		KBID         string `json:"kbId,omitempty"`
	}

	CreateSessionAndSendMessageResponse struct {
		AiInformation string              `json:"Information,omitempty"`
		SessionID     string              `json:"sessionId,omitempty"`
		Retrieval     *rag.RetrievalTrace `json:"retrieval,omitempty"`
		controller.Response
	}

	ChatSendRequest struct {
		UserQuestion string `json:"question" binding:"required"`
		ModelType    string `json:"modelType" binding:"required"`
		SessionID    string `json:"sessionId,omitempty" binding:"required"`
		KBID         string `json:"kbId,omitempty"`
	}

	ChatSendResponse struct {
		AiInformation string              `json:"Information,omitempty"`
		Retrieval     *rag.RetrievalTrace `json:"retrieval,omitempty"`
		controller.Response
	}

	ChatHistoryRequest struct {
		SessionID string `json:"sessionId,omitempty" binding:"required"`
	}
	ChatHistoryResponse struct {
		History []model.History `json:"history"`
		controller.Response
	}

	RetrieveDebugRequest struct {
		Query string `json:"query" binding:"required"`
		KBID  string `json:"kbId,omitempty"`
		TopK  int    `json:"topK,omitempty"`
	}

	RetrieveDebugResponse struct {
		Retrieval *rag.RetrievalTrace `json:"retrieval,omitempty"`
		controller.Response
	}
)

func GetUserSessionsByUserEmail(c *gin.Context) {
	res := new(GetUserSessionsResponse)
	userEmail := c.GetString("userEmail")

	userSessions, err := session.GetUserSessionsByUserEmail(userEmail)
	if err != nil {
		c.JSON(http.StatusOK, res.CodeOf(code.CodeServerBusy))
		return
	}

	res.Success()
	res.Sessions = userSessions
	c.JSON(http.StatusOK, res)
}

func CreateSessionAndSendMessage(c *gin.Context) {
	req := new(CreateSessionAndSendMessageRequest)
	res := new(CreateSessionAndSendMessageResponse)
	userEmail := c.GetString("userEmail")
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidParams))
		return
	}

	sessionID, aiInformation, retrieval, code_ := session.CreateSessionAndSendMessage(userEmail, req.UserQuestion, req.ModelType, req.KBID)
	if code_ != code.CodeSuccess {
		c.JSON(http.StatusOK, res.CodeOf(code_))
		return
	}

	res.Success()
	res.AiInformation = aiInformation
	res.SessionID = sessionID
	res.Retrieval = retrieval
	c.JSON(http.StatusOK, res)
}

func CreateStreamSessionAndSendMessage(c *gin.Context) {
	req := new(CreateSessionAndSendMessageRequest)
	userEmail := c.GetString("userEmail")
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(http.StatusOK, gin.H{"error": "Invalid parameters"})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("X-Accel-Buffering", "no")

	sessionID, code_ := session.CreateStreamSessionOnly(userEmail, req.UserQuestion)
	if code_ != code.CodeSuccess {
		c.SSEvent("error", gin.H{"message": "Failed to create session"})
		return
	}

	c.Writer.WriteString(fmt.Sprintf("data: {\"sessionId\": \"%s\"}\n\n", sessionID))
	c.Writer.Flush()

	code_ = session.StreamMessageToExistingSession(userEmail, sessionID, req.UserQuestion, req.ModelType, req.KBID, http.ResponseWriter(c.Writer))
	if code_ != code.CodeSuccess {
		c.SSEvent("error", gin.H{"message": "Failed to send message"})
		return
	}
}

func ChatSend(c *gin.Context) {
	req := new(ChatSendRequest)
	res := new(ChatSendResponse)
	userEmail := c.GetString("userEmail")
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidParams))
		return
	}

	aiInformation, retrieval, code_ := session.ChatSend(userEmail, req.SessionID, req.UserQuestion, req.ModelType, req.KBID)
	if code_ != code.CodeSuccess {
		c.JSON(http.StatusOK, res.CodeOf(code_))
		return
	}

	res.Success()
	res.AiInformation = aiInformation
	res.Retrieval = retrieval
	c.JSON(http.StatusOK, res)
}

func ChatStreamSend(c *gin.Context) {
	req := new(ChatSendRequest)
	userEmail := c.GetString("userEmail")
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(http.StatusOK, gin.H{"error": "Invalid parameters"})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("X-Accel-Buffering", "no")

	code_ := session.ChatStreamSend(userEmail, req.SessionID, req.UserQuestion, req.ModelType, req.KBID, http.ResponseWriter(c.Writer))
	if code_ != code.CodeSuccess {
		c.SSEvent("error", gin.H{"message": "Failed to send message"})
		return
	}
}

func ChatHistory(c *gin.Context) {
	req := new(ChatHistoryRequest)
	res := new(ChatHistoryResponse)
	userEmail := c.GetString("userEmail")
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidParams))
		return
	}

	history, code_ := session.GetChatHistory(userEmail, req.SessionID)
	if code_ != code.CodeSuccess {
		c.JSON(http.StatusOK, res.CodeOf(code_))
		return
	}

	res.Success()
	res.History = history
	c.JSON(http.StatusOK, res)
}

func RetrieveDebug(c *gin.Context) {
	req := new(RetrieveDebugRequest)
	res := new(RetrieveDebugResponse)
	userEmail := c.GetString("userEmail")
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidParams))
		return
	}

	retrieval, code_ := session.RetrieveDebug(userEmail, req.Query, req.KBID, req.TopK)
	if code_ != code.CodeSuccess {
		c.JSON(http.StatusOK, res.CodeOf(code_))
		return
	}

	res.Success()
	res.Retrieval = retrieval
	c.JSON(http.StatusOK, res)
}
