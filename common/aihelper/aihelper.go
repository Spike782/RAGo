package aihelper

import (
	"ai-chat/common/rabbitmq"
	"ai-chat/common/rag"
	myredis "ai-chat/common/redis"
	"ai-chat/model"
	"ai-chat/utils"
	"context"
	"log"
	"sync"
)

type AIHelper struct {
	model     AIModel
	messages  []*model.Message
	mu        sync.RWMutex
	SessionID string
	saveFunc  func(*model.Message) (*model.Message, error)
}

func NewAIHelper(aiModel AIModel, SessionID string) *AIHelper {
	return &AIHelper{
		model:     aiModel,
		SessionID: SessionID,
		messages:  make([]*model.Message, 0),
		saveFunc: func(msg *model.Message) (*model.Message, error) {
			data := rabbitmq.GenerateMessageMQParam(msg.SessionID, msg.Content, msg.UserName, msg.IsUser)
			err := rabbitmq.RMQMessage.Publish(data)
			return msg, err
		},
	}
}

func (a *AIHelper) AddMessage(content string, userEmail string, isUser bool, save bool) {
	msg := &model.Message{
		SessionID: a.SessionID,
		Content:   content,
		UserName:  userEmail,
		IsUser:    isUser,
	}

	a.mu.Lock()
	a.messages = append(a.messages, msg)
	a.mu.Unlock()

	if save && a.saveFunc != nil {
		if _, err := a.saveFunc(msg); err != nil {
			log.Printf("save message to mq failed, session=%s err=%v", a.SessionID, err)
		}
	}

	_ = myredis.AppendSessionHistoryCache(a.SessionID, model.History{
		IsUser:  isUser,
		Content: content,
	})
}

func (a *AIHelper) SetSaveFunc(saveFunc func(*model.Message) (*model.Message, error)) {
	a.saveFunc = saveFunc
}

func (a *AIHelper) GetMessages() []*model.Message {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]*model.Message, len(a.messages))
	copy(out, a.messages)
	return out
}

func (a *AIHelper) buildMessagesForLLM(mode string) []*model.Message {
	a.mu.Lock()
	defer a.mu.Unlock()

	trimmed, usage := trimModelMessagesForInput(a.messages)
	a.messages = trimmed
	log.Printf("[token] session=%s mode=%s total=%d used=%d budget=%d trimmed=%v dropped=%d",
		a.SessionID, mode, usage.TotalTokens, usage.UsedTokens, usage.InputBudget, usage.Trimmed, usage.DroppedMessages)

	out := make([]*model.Message, len(a.messages))
	copy(out, a.messages)
	return out
}

func (a *AIHelper) GenerateResponse(userEmail string, ctx context.Context, userQuestion string) (*model.Message, error) {
	a.AddMessage(userQuestion, userEmail, true, true)

	modelMessages := a.buildMessagesForLLM("generate")
	messages := utils.ConvertToSchemaMessages(modelMessages)

	schemaMsg, err := a.model.GenerateResponse(ctx, messages)
	if err != nil {
		return nil, err
	}

	modelMsg := utils.ConvertToModelMessage(a.SessionID, userEmail, schemaMsg)
	a.AddMessage(modelMsg.Content, userEmail, false, true)
	return modelMsg, nil
}

func (a *AIHelper) StreamResponse(userEmail string, ctx context.Context, cb StreamCallback, userQuestion string) (*model.Message, error) {
	a.AddMessage(userQuestion, userEmail, true, true)

	modelMessages := a.buildMessagesForLLM("stream")
	messages := utils.ConvertToSchemaMessages(modelMessages)

	content, err := a.model.StreamResponse(ctx, messages, cb)
	if err != nil {
		return nil, err
	}

	modelMsg := &model.Message{
		SessionID: a.SessionID,
		UserName:  userEmail,
		Content:   content,
		IsUser:    false,
	}
	a.AddMessage(modelMsg.Content, userEmail, false, true)
	return modelMsg, nil
}

func (a *AIHelper) GetModelType() string {
	return a.model.GetModelType()
}

func (a *AIHelper) GetLastRetrievalTrace() *rag.RetrievalTrace {
	provider, ok := a.model.(RetrievalTraceProvider)
	if !ok {
		return nil
	}
	return provider.GetLastRetrievalTrace()
}
