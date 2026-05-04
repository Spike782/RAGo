package user

import (
	"ai-chat/common/mysql"
	myredis "ai-chat/common/redis"
	"ai-chat/model"
	"ai-chat/utils"
	"context"

	"gorm.io/gorm"
)

const (
	CodeMsg     = "ai-chat验证码如下(验证码2分钟内有效):"
	UserNameMsg = "ai-chat的账号如下:"
)

var ctx = context.Background()

func IsExistEmail(email string) (bool, *model.User) {
	if cachedUser, ok, err := myredis.GetCachedUser(email); err == nil && ok && cachedUser != nil {
		return true, cachedUser
	}

	user, err := mysql.GetUserByEmail(email)
	if err == gorm.ErrRecordNotFound || user == nil {
		return false, nil
	}
	_ = myredis.SetCachedUser(user)
	return true, user
}

func Register(email, password string) (*model.User, bool) {
	if user, err := mysql.InsertUser(&model.User{
		Email:    email,
		Name:     email,
		Username: email,
		Password: utils.MD5(password),
	}); err != nil {
		return nil, false
	} else {
		_ = myredis.SetCachedUser(user)
		return user, true
	}
}
