package aihelper

import (
	"ai-chat/common/rabbitmq"
	"ai-chat/model"
	"ai-chat/utils"
	"context"
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
	userMsg := model.Message{
		SessionID: a.SessionID,
		Content:   content,
		UserName:  userEmail,
		IsUser:    isUser,
	}
	a.messages = append(a.messages, &userMsg)
	if save {
		a.saveFunc(&userMsg)
	}
}

// SaveMessage 保存消息到数据库（通过回调函数避免循环依赖）
// 通过传入func，自己调用外部的保存函数，即可支持同步异步等多种策略
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

// 同步生成
func (a *AIHelper) GenerateResponse(userEmail string, ctx context.Context, userQuestion string) (*model.Message, error) {

	//调用存储函数
	a.AddMessage(userQuestion, userEmail, true, true)

	a.mu.RLock()
	//将model.Message转化成schema.Message
	messages := utils.ConvertToSchemaMessages(a.messages)
	a.mu.RUnlock()

	//调用模型生成回复
	schemaMsg, err := a.model.GenerateResponse(ctx, messages)
	if err != nil {
		return nil, err
	}

	//将schema.Message转化成model.Message
	modelMsg := utils.ConvertToModelMessage(a.SessionID, userEmail, schemaMsg)

	//调用存储函数
	a.AddMessage(modelMsg.Content, userEmail, false, true)

	return modelMsg, nil
}

// 流式生成
func (a *AIHelper) StreamResponse(userEmail string, ctx context.Context, cb StreamCallback, userQuestion string) (*model.Message, error) {

	//调用存储函数
	a.AddMessage(userQuestion, userEmail, true, true)

	a.mu.RLock()
	messages := utils.ConvertToSchemaMessages(a.messages)
	a.mu.RUnlock()

	content, err := a.model.StreamResponse(ctx, messages, cb)
	if err != nil {
		return nil, err
	}
	//转化成model.Message
	modelMsg := &model.Message{
		SessionID: a.SessionID,
		UserName:  userEmail,
		Content:   content,
		IsUser:    false,
	}

	//调用存储函数
	a.AddMessage(modelMsg.Content, userEmail, false, true)

	return modelMsg, nil
}

// GetModelType 获取模型类型
func (a *AIHelper) GetModelType() string {
	return a.model.GetModelType()
}
