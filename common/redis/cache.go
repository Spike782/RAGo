package redis

import (
	"ai-chat/model"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	redisCli "github.com/redis/go-redis/v9"
)

const (
	modelConfigTTL    = 30 * time.Minute
	userCacheTTL      = 30 * time.Minute
	sessionHistoryTTL = 12 * time.Hour
	sessionHistoryMax = 300
)

func modelConfigKey(modelType string) string {
	return fmt.Sprintf("cache:model_config:%s", strings.TrimSpace(modelType))
}

func userInfoKey(email string) string {
	return fmt.Sprintf("cache:user:%s", strings.ToLower(strings.TrimSpace(email)))
}

func sessionHistoryKey(sessionID string) string {
	return fmt.Sprintf("cache:session_history:%s", strings.TrimSpace(sessionID))
}

type CachedModelConfig struct {
	ModelName string `json:"model_name"`
	BaseURL   string `json:"base_url"`
}

func SetCachedModelConfig(modelType, modelName, baseURL string) error {
	if Rdb == nil {
		return nil
	}
	raw, err := json.Marshal(CachedModelConfig{
		ModelName: modelName,
		BaseURL:   baseURL,
	})
	if err != nil {
		return err
	}
	return Rdb.Set(ctx, modelConfigKey(modelType), raw, modelConfigTTL).Err()
}

func GetCachedModelConfig(modelType string) (*CachedModelConfig, bool, error) {
	if Rdb == nil {
		return nil, false, nil
	}
	raw, err := Rdb.Get(ctx, modelConfigKey(modelType)).Result()
	if err != nil {
		if err == redisCli.Nil {
			return nil, false, nil
		}
		return nil, false, err
	}
	var conf CachedModelConfig
	if err := json.Unmarshal([]byte(raw), &conf); err != nil {
		return nil, false, err
	}
	return &conf, true, nil
}

func SetCachedUser(user *model.User) error {
	if Rdb == nil || user == nil || strings.TrimSpace(user.Email) == "" {
		return nil
	}
	raw, err := json.Marshal(user)
	if err != nil {
		return err
	}
	return Rdb.Set(ctx, userInfoKey(user.Email), raw, userCacheTTL).Err()
}

func GetCachedUser(email string) (*model.User, bool, error) {
	if Rdb == nil {
		return nil, false, nil
	}
	raw, err := Rdb.Get(ctx, userInfoKey(email)).Result()
	if err != nil {
		if err == redisCli.Nil {
			return nil, false, nil
		}
		return nil, false, err
	}
	var user model.User
	if err := json.Unmarshal([]byte(raw), &user); err != nil {
		return nil, false, err
	}
	return &user, true, nil
}

func SetSessionHistoryCache(sessionID string, history []model.History) error {
	if Rdb == nil || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	key := sessionHistoryKey(sessionID)

	pipe := Rdb.TxPipeline()
	pipe.Del(ctx, key)
	if len(history) > 0 {
		values := make([]interface{}, 0, len(history))
		for _, item := range history {
			raw, err := json.Marshal(item)
			if err != nil {
				return err
			}
			values = append(values, string(raw))
		}
		pipe.RPush(ctx, key, values...)
		pipe.LTrim(ctx, key, -sessionHistoryMax, -1)
	}
	pipe.Expire(ctx, key, sessionHistoryTTL)
	_, err := pipe.Exec(ctx)
	return err
}

func AppendSessionHistoryCache(sessionID string, history model.History) error {
	if Rdb == nil || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	raw, err := json.Marshal(history)
	if err != nil {
		return err
	}

	key := sessionHistoryKey(sessionID)
	pipe := Rdb.TxPipeline()
	pipe.RPush(ctx, key, string(raw))
	pipe.LTrim(ctx, key, -sessionHistoryMax, -1)
	pipe.Expire(ctx, key, sessionHistoryTTL)
	_, err = pipe.Exec(ctx)
	return err
}

func GetSessionHistoryCache(sessionID string) ([]model.History, bool, error) {
	if Rdb == nil || strings.TrimSpace(sessionID) == "" {
		return nil, false, nil
	}
	key := sessionHistoryKey(sessionID)
	values, err := Rdb.LRange(ctx, key, 0, -1).Result()
	if err != nil {
		if err == redisCli.Nil {
			return nil, false, nil
		}
		return nil, false, err
	}
	if len(values) == 0 {
		return nil, false, nil
	}

	history := make([]model.History, 0, len(values))
	for _, item := range values {
		var h model.History
		if err := json.Unmarshal([]byte(item), &h); err != nil {
			continue
		}
		history = append(history, h)
	}
	return history, len(history) > 0, nil
}

func WarmSessionHistoryCache(sessionID string, msgs []*model.Message) error {
	history := make([]model.History, 0, len(msgs))
	for _, msg := range msgs {
		if msg == nil {
			continue
		}
		history = append(history, model.History{
			IsUser:  msg.IsUser,
			Content: msg.Content,
		})
	}
	return SetSessionHistoryCache(sessionID, history)
}

func GetRedisContext() context.Context {
	return ctx
}
