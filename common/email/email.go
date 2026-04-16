package email

import (
	"ai-chat/config"
	"fmt"

	"gopkg.in/gomail.v2"
)

const (
	CodeMsg     = "ai-chat验证码如下(验证码2分钟内有效):"
	UserNameMsg = "ai-chat的账号如下:"
)

func SendCaptcha(email, code, msg string) error {

	m := gomail.NewMessage()
	m.SetHeader("From", config.GetConfig().EmailConfig.Email)
	m.SetHeader("To", email)

	m.SetHeader("Subject", "来自ai-chat的消息")

	m.SetBody("text/plain", msg+" "+code)

	d := gomail.NewDialer("smtp.163.com", 465, config.GetConfig().EmailConfig.Email, config.GetConfig().EmailConfig.Authcode)
	if err := d.DialAndSend(m); err != nil {
		fmt.Printf("DialAndSend err %v:\n", err)
		return err
	}
	fmt.Printf("send email success\n")
	return nil
}
