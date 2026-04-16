package redis

import (
	"ai-chat/config"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	redisCli "github.com/redis/go-redis/v9"
)

var Rdb *redisCli.Client
var ctx = context.Background()

func Init() {
	conf := config.GetConfig()
	host := conf.RedisConfig.RedisHost
	port := conf.RedisConfig.RedisPort
	password := conf.RedisConfig.RedisPassword
	db := conf.RedisDb
	addr := host + ":" + strconv.Itoa(port)

	Rdb = redisCli.NewClient(&redisCli.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
		Protocol: 2,
	})
}

func SetCaptchaForEmail(email, captcha string) error {
	key := GenerateCaptcha(email)
	expire := 2 * time.Minute
	return Rdb.Set(ctx, key, captcha, expire).Err()
}

func CheckCaptchaForEmail(email, userInput string) (bool, error) {
	key := GenerateCaptcha(email)

	storedCptcha, err := Rdb.Get(ctx, key).Result()
	if err != nil {
		if err == redisCli.Nil {
			return false, nil
		}
		return false, err
	}
	if strings.EqualFold(storedCptcha, userInput) {
		if err := Rdb.Del(ctx, key).Err(); err != nil {

		} else {

		}
		return true, nil
	}
	return false, nil
}

// InitRedisIndex 初始化 Redis 索引，支持按文件名区分
func InitRedisIndex(ctx context.Context, filename string, dimension int) error {
	indexName := GenerateIndexName(filename)

	// 检查索引是否存在
	_, err := Rdb.Do(ctx, "FT.INFO", indexName).Result()
	if err == nil {
		fmt.Println("索引已存在，跳过创建")
		return nil
	}

	// 如果索引不存在，创建新索引
	if !strings.Contains(err.Error(), "Unknown index name") {
		return fmt.Errorf("检查索引失败: %w", err)
	}

	fmt.Println("正在创建 Redis 索引...")

	prefix := GenerateIndexNamePrefix(filename)

	// 创建索引
	createArgs := []interface{}{
		"FT.CREATE", indexName,
		"ON", "HASH",
		"PREFIX", "1", prefix,
		"SCHEMA",
		"content", "TEXT",
		"metadata", "TEXT",
		"vector", "VECTOR", "FLAT",
		"6",
		"TYPE", "FLOAT32",
		"DIM", dimension,
		"DISTANCE_METRIC", "COSINE",
	}

	if err := Rdb.Do(ctx, createArgs...).Err(); err != nil {
		return fmt.Errorf("创建索引失败: %w", err)
	}

	fmt.Println("索引创建成功！")
	return nil
}

// DeleteRedisIndex 删除 Redis 索引，支持按文件名区分
func DeleteRedisIndex(ctx context.Context, filename string) error {
	indexName := GenerateIndexName(filename)

	// 删除索引
	if err := Rdb.Do(ctx, "FT.DROPINDEX", indexName).Err(); err != nil {
		return fmt.Errorf("删除索引失败: %w", err)
	}

	fmt.Println("索引删除成功！")
	return nil
}
