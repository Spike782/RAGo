package session

import (
	"ai-chat/common/aihelper"
	"ai-chat/common/code"
	"ai-chat/common/rag"
	myredis "ai-chat/common/redis"
	"ai-chat/dao/message"
	"ai-chat/dao/session"
	"ai-chat/model"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

var ctx = context.Background()

func buildModelConfig(userEmail, kbID string) map[string]interface{} {
	return map[string]interface{}{
		"email": userEmail,
		"kbId":  rag.NormalizeKnowledgeBaseID(kbID),
	}
}

func GetUserSessionsByUserEmail(userEmail string) ([]model.SessionInfo, error) {
	sessions, err := session.GetSessionsByUserEmail(userEmail)
	if err != nil {
		return nil, err
	}

	infos := make([]model.SessionInfo, 0, len(sessions))
	for _, s := range sessions {
		title := strings.TrimSpace(s.Title)
		if title == "" {
			title = s.ID
		}
		infos = append(infos, model.SessionInfo{
			SessionID: s.ID,
			Title:     title,
		})
	}
	return infos, nil
}

func CreateSessionAndSendMessage(userEmail, userQuestion, modelType, kbID string) (string, string, *rag.RetrievalTrace, code.Code) {
	newSession := &model.Session{
		ID:       uuid.New().String(),
		UserName: userEmail,
		Title:    userQuestion,
	}
	createdSession, err := session.CreateSession(newSession)
	if err != nil {
		log.Println("CreateSessionAndSendMessage CreateSession error:", err)
		return "", "", nil, code.CodeServerBusy
	}

	manager := aihelper.GetGlobalManager()
	helper, err := manager.GetOrCreateAIHelper(userEmail, createdSession.ID, modelType, buildModelConfig(userEmail, kbID))
	if err != nil {
		log.Println("CreateSessionAndSendMessage GetOrCreateAIHelper error:", err)
		return "", "", nil, code.AIModelFail
	}

	aiResponse, err := helper.GenerateResponse(userEmail, ctx, userQuestion)
	if err != nil {
		log.Println("CreateSessionAndSendMessage GenerateResponse error:", err)
		return "", "", nil, code.AIModelFail
	}

	return createdSession.ID, aiResponse.Content, helper.GetLastRetrievalTrace(), code.CodeSuccess
}

func CreateStreamSessionOnly(userEmail, userQuestion string) (string, code.Code) {
	newSession := &model.Session{
		ID:       uuid.New().String(),
		UserName: userEmail,
		Title:    userQuestion,
	}
	createdSession, err := session.CreateSession(newSession)
	if err != nil {
		log.Println("CreateStreamSessionOnly CreateSession error:", err)
		return "", code.CodeServerBusy
	}
	return createdSession.ID, code.CodeSuccess
}

// Stream response to an existing session via SSE.
func StreamMessageToExistingSession(userEmail, sessionID, userQuestion, modelType, kbID string, writer http.ResponseWriter) code.Code {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		log.Println("StreamMessageToExistingSession: streaming unsupported")
		return code.CodeServerBusy
	}

	manager := aihelper.GetGlobalManager()
	helper, err := manager.GetOrCreateAIHelper(userEmail, sessionID, modelType, buildModelConfig(userEmail, kbID))
	if err != nil {
		log.Println("StreamMessageToExistingSession GetOrCreateAIHelper error:", err)
		return code.AIModelFail
	}

	cb := func(msg string) {
		_, werr := writer.Write([]byte("data: " + msg + "\n\n"))
		if werr != nil {
			log.Println("[SSE] Write error:", werr)
			return
		}
		flusher.Flush()
	}

	if _, err := helper.StreamResponse(userEmail, ctx, cb, userQuestion); err != nil {
		log.Println("StreamMessageToExistingSession StreamResponse error:", err)
		return code.AIModelFail
	}

	if trace := helper.GetLastRetrievalTrace(); trace != nil && trace.Enabled {
		if raw, err := json.Marshal(trace); err == nil {
			if _, err := writer.Write([]byte("event: retrieval\ndata: " + string(raw) + "\n\n")); err != nil {
				log.Println("StreamMessageToExistingSession write retrieval event error:", err)
			} else {
				flusher.Flush()
			}
		}
	}

	if _, err := writer.Write([]byte("data: [DONE]\n\n")); err != nil {
		log.Println("StreamMessageToExistingSession write DONE error:", err)
		return code.AIModelFail
	}
	flusher.Flush()

	return code.CodeSuccess
}

func CreateStreamSessionAndSendMessage(userEmail, userQuestion, modelType, kbID string, writer http.ResponseWriter) (string, code.Code) {
	sessionID, code_ := CreateStreamSessionOnly(userEmail, userQuestion)
	if code_ != code.CodeSuccess {
		return "", code_
	}

	code_ = StreamMessageToExistingSession(userEmail, sessionID, userQuestion, modelType, kbID, writer)
	if code_ != code.CodeSuccess {
		return sessionID, code_
	}
	return sessionID, code.CodeSuccess
}

func ChatSend(userEmail, sessionID, userQuestion, modelType, kbID string) (string, *rag.RetrievalTrace, code.Code) {
	manager := aihelper.GetGlobalManager()
	helper, err := manager.GetOrCreateAIHelper(userEmail, sessionID, modelType, buildModelConfig(userEmail, kbID))
	if err != nil {
		log.Println("ChatSend GetOrCreateAIHelper error:", err)
		return "", nil, code.AIModelFail
	}

	aiResponse, err := helper.GenerateResponse(userEmail, ctx, userQuestion)
	if err != nil {
		log.Println("ChatSend GenerateResponse error:", err)
		return "", nil, code.AIModelFail
	}

	return aiResponse.Content, helper.GetLastRetrievalTrace(), code.CodeSuccess
}

func GetChatHistory(userEmail, sessionID string) ([]model.History, code.Code) {
	manager := aihelper.GetGlobalManager()
	helper, exists := manager.GetAIHelper(userEmail, sessionID)
	if exists {
		messages := helper.GetMessages()
		history := make([]model.History, 0, len(messages))
		for _, msg := range messages {
			history = append(history, model.History{
				IsUser:  msg.IsUser,
				Content: msg.Content,
			})
		}
		_ = myredis.SetSessionHistoryCache(sessionID, history)
		return history, code.CodeSuccess
	}

	if history, ok, err := myredis.GetSessionHistoryCache(sessionID); err == nil && ok {
		return history, code.CodeSuccess
	}

	msgs, err := message.GetMessagesBySessionID(sessionID)
	if err != nil {
		log.Println("GetChatHistory load db error:", err)
		return nil, code.CodeServerBusy
	}
	history := make([]model.History, 0, len(msgs))
	for _, msg := range msgs {
		history = append(history, model.History{
			IsUser:  msg.IsUser,
			Content: msg.Content,
		})
	}
	_ = myredis.SetSessionHistoryCache(sessionID, history)
	return history, code.CodeSuccess
}

func ChatStreamSend(userEmail, sessionID, userQuestion, modelType, kbID string, writer http.ResponseWriter) code.Code {
	return StreamMessageToExistingSession(userEmail, sessionID, userQuestion, modelType, kbID, writer)
}

func RetrieveDebug(userEmail, query, kbID string, topK int) (*rag.RetrievalTrace, code.Code) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, code.CodeInvalidParams
	}

	start := time.Now()
	ragQuery, err := rag.NewRAGQueryWithTopK(ctx, userEmail, kbID, topK)
	if err != nil {
		return rag.BuildRetrievalErrorTrace(query, rag.NormalizeKnowledgeBaseID(kbID), "", topK, time.Since(start), err), code.CodeSuccess
	}

	docs, err := ragQuery.RetrieveDocuments(ctx, query)
	if err != nil {
		return rag.BuildRetrievalErrorTrace(query, ragQuery.KnowledgeBaseID(), ragQuery.StoreName(), ragQuery.TopK(), time.Since(start), err), code.CodeSuccess
	}
	return rag.BuildRetrievalTrace(query, ragQuery.KnowledgeBaseID(), ragQuery.StoreName(), ragQuery.TopK(), docs, time.Since(start)), code.CodeSuccess
}
