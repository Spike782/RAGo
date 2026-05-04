package aihelper

import (
	myredis "ai-chat/common/redis"
	"os"
	"strings"
)

func resolveModelConfig(modelType, fallbackModelName, fallbackBaseURL string) (string, string) {
	modelName := strings.TrimSpace(os.Getenv("OPENAI_MODEL_NAME"))
	baseURL := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL"))
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv("OPEN_AI_BASE_URL"))
	}

	if cached, ok, err := myredis.GetCachedModelConfig(modelType); err == nil && ok && cached != nil {
		if modelName == "" {
			modelName = strings.TrimSpace(cached.ModelName)
		}
		if baseURL == "" {
			baseURL = strings.TrimSpace(cached.BaseURL)
		}
	}

	if modelName == "" {
		modelName = strings.TrimSpace(fallbackModelName)
	}
	if baseURL == "" {
		baseURL = strings.TrimSpace(fallbackBaseURL)
	}

	if modelName != "" && baseURL != "" {
		_ = myredis.SetCachedModelConfig(modelType, modelName, baseURL)
	}
	return modelName, baseURL
}
