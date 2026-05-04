package router

import (
	"ai-chat/middleware/jwt"
	"ai-chat/middleware/ratelimit"

	"github.com/gin-gonic/gin"
)

func InitRouter() *gin.Engine {

	r := gin.Default()
	enterRouter := r.Group("/api/v1")
	{
		RegisterUserRouter(enterRouter.Group("/user"))
	}
	{
		UserAuthGroup := enterRouter.Group("/user")
		UserAuthGroup.Use(jwt.Auth(), ratelimit.AuthRateLimit())
		RegisterUserAuthRouter(UserAuthGroup)
	}
	//后续登录的接口需要jwt鉴权
	{
		AIGroup := enterRouter.Group("/AI")
		AIGroup.Use(jwt.Auth(), ratelimit.AuthRateLimit())
		AIRouter(AIGroup)
	}

	{
		ImageGroup := enterRouter.Group("/image")
		ImageGroup.Use(jwt.Auth(), ratelimit.AuthRateLimit())
		ImageRouter(ImageGroup)
	}

	{
		FileGroup := enterRouter.Group("/file")
		FileGroup.Use(jwt.Auth(), ratelimit.AuthRateLimit())
		FileRouter(FileGroup)
	}

	return r
}
