package jwt

import (
	"ai-chat/common/code"
	myredis "ai-chat/common/redis"
	"ai-chat/controller"
	"ai-chat/utils/jwt"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		res := new(controller.Response)

		var token string
		authHeader := c.GetHeader("Authorization")
		if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
			token = strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
		} else {
			token = strings.TrimSpace(c.Query("token"))
		}

		if token == "" {
			c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidToken))
			c.Abort()
			return
		}

		log.Println("token is ", token)
		userEmail, ok := jwt.ParseToken(token)
		if !ok {
			c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidToken))
			c.Abort()
			return
		}

		c.Set("userEmail", userEmail)
		if err := myredis.SetUserOnline(userEmail); err != nil {
			log.Printf("set user online failed: %v", err)
		}
		c.Next()
	}
}
