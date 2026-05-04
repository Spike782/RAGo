package router

import (
	"ai-chat/controller/session"
	"ai-chat/controller/tts"

	"github.com/gin-gonic/gin"
)

func AIRouter(r *gin.RouterGroup) {

	// 鑱婂ぉ鐩稿叧鎺ュ彛
	{
		r.GET("/chat/sessions", session.GetUserSessionsByUserEmail)
		r.POST("/chat/send-new-session", session.CreateSessionAndSendMessage)
		r.POST("/chat/send", session.ChatSend)
		r.POST("/chat/history", session.ChatHistory)
		r.POST("/chat/retrieve-debug", session.RetrieveDebug)

		// TTS鐩稿叧鎺ュ彛
		r.POST("/chat/tts", tts.CreateTTSTask)
		r.GET("/chat/tts/query", tts.QueryTTSTask)

		r.POST("/chat/send-stream-new-session", session.CreateStreamSessionAndSendMessage)
		r.POST("/chat/send-stream", session.ChatStreamSend)
	}

}
