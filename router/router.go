package router

import (
	"ai-chat/middleware/jwt"

	"github.com/gin-gonic/gin"
)

func InitRouter() *gin.Engine {

	r := gin.Default()
	enterRouter := r.Group("/api/v1")
	{
		RegisterUserRouter(enterRouter.Group("/user"))
	}
	//后续登录的接口需要jwt鉴权
	{
		AIGroup := enterRouter.Group("/AI")
		AIGroup.Use(jwt.Auth())
		AIRouter(AIGroup)
	}

	{
		ImageGroup := enterRouter.Group("/image")
		ImageGroup.Use(jwt.Auth())
		ImageRouter(ImageGroup)
	}

	{
		FileGroup := enterRouter.Group("/file")
		FileGroup.Use(jwt.Auth())
		FileRouter(FileGroup)
	}

	return r
}
