package config

import (
	"errors"
	"os"
	"time"
)

// ErrMissingEncKey is returned when KS_ENC_KEY is not set.
var ErrMissingEncKey = errors.New("缺少 KS_ENC_KEY 环境变量(32 字节 AES 密钥); 请设置 KS_ENC_KEY=your-32-byte-key 后重启")

// Config holds runtime configuration loaded from environment variables.
type Config struct {
	Addr       string // HTTP listen address
	DBDriver   string // sqlite | mysql | postgres
	DSN        string // GORM data source name
	SessionTTL time.Duration
	EncKey     []byte // AES-GCM key for provider api_key encryption (32 bytes)
	DBPath     string // sqlite file path (used when DBDriver=sqlite)
	ExecTimeout time.Duration // bounds every HTTP call and whole-run execution
}

// Load reads configuration from environment. Returns error when required
// variables are missing (e.g. KS_ENC_KEY).
func Load() (*Config, error) {
	driver := env("DB_DRIVER", "sqlite")
	dsn := env("DB_DSN", "")
	if driver == "sqlite" && dsn == "" {
		dsn = env("DB_PATH", "kianshu.db")
	}
	encKey := env("KS_ENC_KEY", "")
	if encKey == "" {
		return nil, ErrMissingEncKey
	}
	return &Config{
		Addr:        env("KS_ADDR", ":8080"),
		DBDriver:    driver,
		DSN:         dsn,
		SessionTTL:  time.Duration(envInt("KS_SESSION_TTL_HOURS", 168)) * time.Hour,
		EncKey:      []byte(encKey),
		DBPath:      dsn,
		ExecTimeout: time.Duration(envInt("KS_EXEC_TIMEOUT", 60)) * time.Second,
	}, nil
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	v, ok := os.LookupEnv(key)
	if !ok {
		return def
	}
	n := 0
	for _, c := range v {
		if c < '0' || c > '9' {
			return def
		}
		n = n*10 + int(c-'0')
	}
	return n
}
