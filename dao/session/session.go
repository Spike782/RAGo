package session

import (
	"ai-chat/common/mysql"
	"ai-chat/model"
)

func GetSessionsByUserEmail(userEmail string) ([]model.Session, error) {
	var sessions []model.Session
	err := mysql.DB.Where("user_name = ?", userEmail).Order("created_at desc").Find(&sessions).Error
	return sessions, err
}

func CreateSession(session *model.Session) (*model.Session, error) {
	err := mysql.DB.Create(session).Error
	return session, err
}

func GetSessionByID(sessionID string) (*model.Session, error) {
	var session model.Session
	err := mysql.DB.Where("id = ?", sessionID).First(&session).Error
	return &session, err
}
