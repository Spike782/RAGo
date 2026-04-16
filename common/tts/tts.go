package tts

import (
	"ai-chat/config"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
)

type TTSService struct{}

func NewTTSService() *TTSService {
	return &TTSService{}
}

type TTSRequest struct {
	Text           string `json:"text"`
	Format         string `json:"format"`
	Voice          int    `json:"voice"`
	Lang           string `json:"lang"`
	Speed          int    `json:"speed"`
	Pitch          int    `json:"pitch"`
	Volume         int    `json:"volume"`
	EnableSubtitle int    `json:"enable_subtitle"`
}

type TTSCreateResponse struct {
	TaskID string `json:"task_id"`
}

func (s *TTSService) CreateTTS(ctx context.Context, text string) (string, error) {
	accessToken := s.GetAccessToken()
	if accessToken == "" {
		return "", fmt.Errorf("failed to get access token")
	}

	payload := TTSRequest{
		Text:           text,
		Format:         "mp3-16k",
		Voice:          4194,
		Lang:           "zh",
		Speed:          5,
		Pitch:          5,
		Volume:         5,
		EnableSubtitle: 0,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	url := "https://aip.baidubce.com/rpc/2.0/tts/v1/create?access_token=" + accessToken
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	log.Println("[TTS Create] raw:", string(respBody))

	var result TTSCreateResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", err
	}

	if result.TaskID == "" {
		return "", fmt.Errorf("create tts failed: empty task_id")
	}

	return result.TaskID, nil
}

func (s *TTSService) GetAccessToken() string {
	conf := config.GetConfig()

	url := "https://aip.baidubce.com/oauth/2.0/token"
	postData := fmt.Sprintf(
		"grant_type=client_credentials&client_id=%s&client_secret=%s",
		conf.VoiceServiceConfig.VoiceServiceApiKey,
		conf.VoiceServiceConfig.VoiceServiceSecretKey,
	)

	resp, err := http.Post(url, "application/x-www-form-urlencoded", bytes.NewReader([]byte(postData)))
	if err != nil {
		log.Println("get token error:", err)
		return ""
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Println("read token error:", err)
		return ""
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
	}

	if err := json.Unmarshal(body, &tokenResp); err != nil {
		log.Println("unmarshal token error:", err)
		return ""
	}

	return tokenResp.AccessToken
}

type TTSTaskResult struct {
	SpeechURL string `json:"speech_url,omitempty"`
}

type TTSTask struct {
	TaskID     string         `json:"task_id"`
	TaskStatus string         `json:"task_status"`
	TaskResult *TTSTaskResult `json:"task_result,omitempty"`
}

type TTSQueryResponse struct {
	LogID     string    `json:"log_id"`
	TasksInfo []TTSTask `json:"tasks_info"`
}

// QueryTTSFull 查询官方 TTS 状态，解析完整 JSON
func (s *TTSService) QueryTTSFull(ctx context.Context, taskID string) (*TTSQueryResponse, error) {
	accessToken := s.GetAccessToken()
	if accessToken == "" {
		return nil, fmt.Errorf("failed to get access token")
	}

	reqBody := map[string][]string{
		"task_ids": {taskID},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	url := "https://aip.baidubce.com/rpc/2.0/tts/v1/query?access_token=" + accessToken
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	log.Println("[TTS Query] raw:", string(respBody))

	// 官方返回原始 JSON
	var rawResp struct {
		LogID     json.Number `json:"log_id"`
		TasksInfo []struct {
			TaskID     string          `json:"task_id"`
			TaskStatus string          `json:"task_status"`
			TaskResult json.RawMessage `json:"task_result,omitempty"`
		} `json:"tasks_info"`
	}

	if err := json.Unmarshal(respBody, &rawResp); err != nil {
		return nil, err
	}

	result := &TTSQueryResponse{
		LogID:     rawResp.LogID.String(),
		TasksInfo: make([]TTSTask, 0, len(rawResp.TasksInfo)),
	}

	for _, t := range rawResp.TasksInfo {
		task := TTSTask{
			TaskID:     t.TaskID,
			TaskStatus: t.TaskStatus,
			TaskResult: nil, // 默认 nil
		}

		if t.TaskStatus == "Success" && len(t.TaskResult) > 0 {
			var r TTSTaskResult
			if err := json.Unmarshal(t.TaskResult, &r); err != nil {
				log.Println("parse task_result error:", err)
				return nil, fmt.Errorf("failed to parse task result: %v", err)
			}
			task.TaskResult = &r
		}

		result.TasksInfo = append(result.TasksInfo, task)
	}

	return result, nil
}
