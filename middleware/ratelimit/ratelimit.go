package ratelimit

import (
	"ai-chat/common/redis"
	"ai-chat/config"
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	redisCli "github.com/redis/go-redis/v9"
)

const (
	defaultWindowSeconds = 60
	defaultMaxRequests   = 120
	keyPrefix            = "rate_limit"
)

var rateLimitCtx = context.Background()

func AuthRateLimit() gin.HandlerFunc {
	cfg := config.GetConfig().RateLimitConfig
	enabled := cfg.Enabled
	windowSeconds := cfg.WindowSeconds
	maxRequests := cfg.MaxRequests

	if windowSeconds <= 0 {
		windowSeconds = defaultWindowSeconds
	}
	if maxRequests <= 0 {
		maxRequests = defaultMaxRequests
	}

	return func(c *gin.Context) {
		if !enabled || redis.Rdb == nil {
			c.Next()
			return
		}

		identity := c.GetString("userEmail")
		if identity == "" {
			identity = c.ClientIP()
		}

		route := c.FullPath()
		if route == "" {
			route = c.Request.URL.Path
		}

		allowed, remaining, resetAt, err := allow(identity, route, windowSeconds, maxRequests)
		if err != nil {
			log.Printf("rate limit redis error: %v", err)
			c.Next()
			return
		}

		c.Header("X-RateLimit-Limit", strconv.Itoa(maxRequests))
		c.Header("X-RateLimit-Remaining", strconv.Itoa(maxInt(remaining, 0)))
		c.Header("X-RateLimit-Reset", strconv.FormatInt(resetAt, 10))

		if !allowed {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"status_code": 4290,
				"status_msg":  "Too many requests, please retry later",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

func allow(identity, route string, windowSeconds, maxRequests int) (bool, int, int64, error) {
	now := time.Now()
	nowMs := now.UnixMilli()
	windowMs := int64(windowSeconds) * 1000

	key := fmt.Sprintf("%s:%s:%s", keyPrefix, identity, route)
	member := fmt.Sprintf("%d-%d", nowMs, time.Now().UnixNano())
	minScore := strconv.FormatInt(nowMs-windowMs, 10)

	pipe := redis.Rdb.TxPipeline()
	pipe.ZRemRangeByScore(rateLimitCtx, key, "-inf", minScore)
	pipe.ZAdd(rateLimitCtx, key, redisCli.Z{Score: float64(nowMs), Member: member})
	countCmd := pipe.ZCard(rateLimitCtx, key)
	pipe.Expire(rateLimitCtx, key, time.Duration(windowSeconds+1)*time.Second)
	_, err := pipe.Exec(rateLimitCtx)
	if err != nil {
		return false, 0, 0, err
	}

	count := int(countCmd.Val())
	allowed := count <= maxRequests
	if !allowed {
		_ = redis.Rdb.ZRem(rateLimitCtx, key, member).Err()
	}

	remaining := maxRequests - count
	if remaining < 0 {
		remaining = 0
	}

	resetAt := now.Unix() + int64(windowSeconds)
	return allowed, remaining, resetAt, nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
