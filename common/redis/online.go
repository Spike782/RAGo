package redis

import (
	"ai-chat/config"
	"strconv"
	"strings"
	"time"

	redisCli "github.com/redis/go-redis/v9"
)

const defaultOnlineTTLSeconds = 120

func onlineTTL() time.Duration {
	ttl := config.GetConfig().OnlineConfig.TTLSeconds
	if ttl <= 0 {
		ttl = defaultOnlineTTLSeconds
	}
	return time.Duration(ttl) * time.Second
}

func IsOnlineEnabled() bool {
	return config.GetConfig().OnlineConfig.Enabled
}

func SetUserOnline(email string) error {
	if Rdb == nil || !IsOnlineEnabled() {
		return nil
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return nil
	}
	key := GenerateOnlineKey(email)
	now := strconv.FormatInt(time.Now().Unix(), 10)
	return Rdb.Set(ctx, key, now, onlineTTL()).Err()
}

func GetUserOnlineStatus(email string) (bool, int64, error) {
	if Rdb == nil || !IsOnlineEnabled() {
		return false, 0, nil
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return false, 0, nil
	}

	key := GenerateOnlineKey(email)
	val, err := Rdb.Get(ctx, key).Result()
	if err != nil {
		if err == redisCli.Nil {
			return false, 0, nil
		}
		return false, 0, err
	}
	lastSeen, _ := strconv.ParseInt(strings.TrimSpace(val), 10, 64)
	return true, lastSeen, nil
}
