package rabbitmq

import (
	"ai-chat/dao/message"
	"ai-chat/model"
	"encoding/json"

	"github.com/streadway/amqp"
)

type MessageMQParam struct {
	SessionID string `json:"session_id"`
	Content   string `json:"content"`
	UserName  string `json:"user_email"`
	IsUser    bool   `json:"is_user"`
}

func GenerateMessageMQParam(sessionID, content, userEmail string, isUser bool) []byte {
	param := MessageMQParam{
		SessionID: sessionID,
		Content:   content,
		UserName:  userEmail,
		IsUser:    isUser,
	}
	data, _ := json.Marshal(param)
	return data
}

func MQMessage(msg *amqp.Delivery) error {
	var param MessageMQParam
	err := json.Unmarshal(msg.Body, &param)
	if err != nil {
		return err
	}
	newMsg := &model.Message{
		SessionID: param.SessionID,
		Content:   param.Content,
		UserName:  param.UserName,
		IsUser:    param.IsUser,
	}
	message.CreateMessage(newMsg)
	return nil
}
