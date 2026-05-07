package config

import (
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	Port  string
	DBUrl string
}

func Load() *Config {
	_ = godotenv.Load()

	return &Config{
		Port:  getEnv("PORT", "8080"),
		DBUrl: getEnv("DATABASE_URL", ""),
	}
}

func getEnv(key, fallback string) string {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	return val
}
