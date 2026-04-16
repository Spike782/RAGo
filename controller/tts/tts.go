package tts

import (
	"ai-chat/common/code"
	"ai-chat/common/tts"
	"ai-chat/controller"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

type (
	TTSRequest struct {
		Text string `json:"text,omitempty"`
	}
	TTSResponse struct {
		TaskID string `json:"task_id,omitempty"`
		controller.Response
	}
	QueryTTSResponse struct {
		TaskID     string `json:"task_id,omitempty"`
		TaskStatus string `json:"task_status,omitempty"`
		TaskResult string `json:"task_result,omitempty"`
		controller.Response
	}
)

type TTSServices struct {
	ttsService *tts.TTSService
}

func NewTTSServices() *TTSServices {
	return &TTSServices{
		ttsService: tts.NewTTSService(),
	}
}

func CreateTTSTask(c *gin.Context) {
	tts := NewTTSServices()
	req := new(TTSRequest)
	res := new(TTSResponse)
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidParams))
		return
	}

	if req.Text == "" {
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidParams))
		return
	}

	// 创建TTS任务并返回任务ID，由前端轮询查询结果
	taskID, err := tts.ttsService.CreateTTS(c, req.Text)
	if err != nil {
		c.JSON(http.StatusOK, res.CodeOf(code.TTSFail))
		return
	}

	res.Success()
	res.TaskID = taskID
	c.JSON(http.StatusOK, res)

}

func QueryTTSTask(c *gin.Context) {
	tts := NewTTSServices()
	res := new(QueryTTSResponse)
	taskID := c.Query("task_id")
	if taskID == "" {
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidParams))
		return
	}

	TTSQueryResponse, err := tts.ttsService.QueryTTSFull(c, taskID)
	if err != nil {
		log.Println("语音合成失败", err.Error())
		c.JSON(http.StatusOK, res.CodeOf(code.TTSFail))
		return
	}

	if len(TTSQueryResponse.TasksInfo) == 0 {
		c.JSON(http.StatusOK, res.CodeOf(code.TTSFail))
		return
	}

	res.Success()
	res.TaskID = TTSQueryResponse.TasksInfo[0].TaskID

	// 检查 TaskResult 是否为 nil，避免空指针异常
	if TTSQueryResponse.TasksInfo[0].TaskResult != nil {
		res.TaskResult = TTSQueryResponse.TasksInfo[0].TaskResult.SpeechURL
	}
	res.TaskStatus = TTSQueryResponse.TasksInfo[0].TaskStatus
	c.JSON(http.StatusOK, res)
}
