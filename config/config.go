package config

import (
	"log"

	"github.com/BurntSushi/toml"
)

type MainConfig struct {
	Port    int    `toml:"port"`
	AppName string `toml:"appName"`
	Host    string `toml:"host"`
}

type EmailConfig struct {
	Authcode string `toml:"authcode"`
	Email    string `toml:"email" `
}

type RedisConfig struct {
	RedisPort     int    `toml:"port"`
	RedisDb       int    `toml:"db"`
	RedisHost     string `toml:"host"`
	RedisPassword string `toml:"password"`
}

type MysqlConfig struct {
	MysqlPort         int    `toml:"port"`
	MysqlHost         string `toml:"host"`
	MysqlUser         string `toml:"user"`
	MysqlPassword     string `toml:"password"`
	MysqlDatabaseName string `toml:"databaseName"`
	MysqlCharset      string `toml:"charset"`
}

type JwtConfig struct {
	ExpireDuration int    `toml:"expire_duration"`
	Issuer         string `toml:"issuer"`
	Subject        string `toml:"subject"`
	Key            string `toml:"key"`
}

type Rabbitmq struct {
	RabbitmqPort     int    `toml:"port"`
	RabbitmqHost     string `toml:"host"`
	RabbitmqUsername string `toml:"username"`
	RabbitmqPassword string `toml:"password"`
	RabbitmqVhost    string `toml:"vhost"`
	RabbitmqRetryMax int    `toml:"retryMax"`
	RetryDelayMs     int    `toml:"retryDelayMs"`
}

type RagModelConfig struct {
	RagEmbeddingModel string  `toml:"embeddingModel"`
	RagEmbeddingURL   string  `toml:"embeddingBaseUrl"`
	RagEmbeddingKey   string  `toml:"embeddingApiKey"`
	RagChatModelName  string  `toml:"chatModelName"`
	RagDocDir         string  `toml:"docDir"`
	RagBaseUrl        string  `toml:"baseUrl"`
	RagDimension      int     `toml:"dimension"`
	RagChunkSize      int     `toml:"chunkSize"`
	RagChunkOverlap   int     `toml:"chunkOverlap"`
	RagTopK           int     `toml:"topK"`
	RagVectorStore    string  `toml:"vectorStore"`
	RagQdrantURL      string  `toml:"qdrantUrl"`
	RagQdrantAPIKey   string  `toml:"qdrantApiKey"`
	RagQdrantTimeoutS int     `toml:"qdrantTimeoutSeconds"`
	RagQdrantPrefix   string  `toml:"qdrantCollectionPrefix"`
	RagHybridEnabled  bool    `toml:"hybridEnabled"`
	RagHybridVecW     float64 `toml:"hybridVectorWeight"`
	RagHybridLexW     float64 `toml:"hybridLexicalWeight"`
	RagRerankEnabled  bool    `toml:"rerankEnabled"`
	RagRerankTopN     int     `toml:"rerankTopN"`
	RagRerankBaseW    float64 `toml:"rerankBaseWeight"`
	RagRerankLexW     float64 `toml:"rerankLexicalWeight"`
	RagRerankPosW     float64 `toml:"rerankPositionWeight"`
	ContextMaxTokens  int     `toml:"contextMaxTokens"`
	ReservedOutputTok int     `toml:"reservedOutputTokens"`
	MinContextMsgNum  int     `toml:"minContextMessages"`
	ReactMaxSteps     int     `toml:"reactMaxSteps"`
}

type VoiceServiceConfig struct {
	VoiceServiceApiKey    string `toml:"voiceServiceApiKey"`
	VoiceServiceSecretKey string `toml:"voiceServiceSecretKey"`
}

type RateLimitConfig struct {
	Enabled       bool `toml:"enabled"`
	WindowSeconds int  `toml:"windowSeconds"`
	MaxRequests   int  `toml:"maxRequests"`
}

type OnlineConfig struct {
	Enabled    bool `toml:"enabled"`
	TTLSeconds int  `toml:"ttlSeconds"`
}

type Config struct {
	EmailConfig        `toml:"emailConfig"`
	RedisConfig        `toml:"redisConfig"`
	MysqlConfig        `toml:"mysqlConfig"`
	JwtConfig          `toml:"jwtConfig"`
	MainConfig         `toml:"mainConfig"`
	Rabbitmq           `toml:"rabbitmqConfig"`
	RagModelConfig     `toml:"ragModelConfig"`
	VoiceServiceConfig `toml:"voiceServiceConfig"`
	RateLimitConfig    `toml:"rateLimitConfig"`
	OnlineConfig       `toml:"onlineConfig"`
}

type RedisKeyConfig struct {
	CaptchaPrefix   string
	IndexName       string
	IndexNamePrefix string
	OnlinePrefix    string
}

var DefaultRedisKeyConfig = RedisKeyConfig{
	CaptchaPrefix:   "captcha:%s",
	IndexName:       "rag_docs:%s:idx",
	IndexNamePrefix: "rag_docs:%s:",
	OnlinePrefix:    "online:user:%s",
}

var config *Config

// InitConfig 初始化项目配置
func InitConfig() error {
	if _, err := toml.DecodeFile("config/config.toml", config); err != nil {
		log.Fatal(err.Error())
		return err
	}
	return nil
}

func GetConfig() *Config {
	if config == nil {
		config = new(Config)
		_ = InitConfig()
	}
	return config
}
