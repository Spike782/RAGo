package user

import (
	"ai-chat/common/code"
	"ai-chat/controller"
	serviceUser "ai-chat/service/user"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type OnlineStatusResponse struct {
	controller.Response
	Email    string `json:"email,omitempty"`
	Online   bool   `json:"online"`
	LastSeen int64  `json:"lastSeen,omitempty"`
}

func OnlineStatus(c *gin.Context) {
	res := new(OnlineStatusResponse)
	targetEmail := strings.TrimSpace(c.Query("email"))
	if targetEmail == "" {
		targetEmail = c.GetString("userEmail")
	}

	online, lastSeen, code_ := serviceUser.GetOnlineStatus(targetEmail)
	if code_ != code.CodeSuccess {
		c.JSON(http.StatusOK, res.CodeOf(code_))
		return
	}

	res.Success()
	res.Email = targetEmail
	res.Online = online
	res.LastSeen = lastSeen
	c.JSON(http.StatusOK, res)
}
