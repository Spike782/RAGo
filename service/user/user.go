package user

import (
	"ai-chat/common/code"
	myemail "ai-chat/common/email"
	myredis "ai-chat/common/redis"
	"ai-chat/dao/user"
	"ai-chat/model"
	"ai-chat/utils"
	myjwt "ai-chat/utils/jwt"
	"strings"
)

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func Login(email, password string) (string, code.Code) {
	var userInformation *model.User
	var ok bool

	email = normalizeEmail(email)
	if ok, userInformation = user.IsExistEmail(email); !ok {
		return "", code.CodeUserNotExist
	}

	if userInformation.Password != utils.MD5(password) {
		return "", code.CodeInvalidPassword
	}

	token, err := myjwt.GenerateToken(userInformation.ID, userInformation.Email)
	if err != nil {
		return "", code.CodeServerBusy
	}
	_ = myredis.SetUserOnline(userInformation.Email)
	return token, code.CodeSuccess
}

func Register(email, password, captcha string) (string, code.Code) {
	var ok bool
	var userInformation *model.User

	email = normalizeEmail(email)

	if ok, _ = user.IsExistEmail(email); ok {
		return "", code.CodeUserExist
	}

	if ok, _ = myredis.CheckCaptchaForEmail(email, captcha); !ok {
		return "", code.CodeInvalidCaptcha
	}

	// Use email as account identifier.
	if userInformation, ok = user.Register(email, password); !ok {
		return "", code.CodeServerBusy
	}

	token, err := myjwt.GenerateToken(userInformation.ID, userInformation.Email)
	if err != nil {
		return "", code.CodeServerBusy
	}
	_ = myredis.SetUserOnline(userInformation.Email)

	return token, code.CodeSuccess
}

func SendCaptcha(email_ string) code.Code {
	email_ = normalizeEmail(email_)
	sendCode := utils.GetRandomNumbers(6)

	if err := myredis.SetCaptchaForEmail(email_, sendCode); err != nil {
		return code.CodeServerBusy
	}

	if err := myemail.SendCaptcha(email_, sendCode, myemail.CodeMsg); err != nil {
		return code.CodeServerBusy
	}

	return code.CodeSuccess
}
