package config

import (
	"errors"
	"log"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	AppEnv string

	AppPort string

	DBURL string

	JWTSecret string

	JWTAccessTokenTTL  time.Duration
	JWTRefreshTokenTTL time.Duration

	CORSAllowedOrigins []string

	LogLevel string
}

// Load loads the config from the environment
func Load() *Config {
	err := godotenv.Load()
	if err != nil {
		log.Println(".env file not found")
	}

	accessTokenTTL, err := time.ParseDuration(getEnv("JWT_ACCESS_TTL", "15m"))
	if err != nil {
		log.Fatal(err)
	}

	refreshTokenTTL, err := time.ParseDuration(getEnv("JWT_REFRESH_TTL", "720h"))
	if err != nil {
		log.Fatal(err)
	}

	return &Config{
		AppEnv:             getEnv("APP_ENV", ""),
		AppPort:            getEnv("APP_PORT", "8080"),
		DBURL:              getEnv("DB_URL", ""),
		JWTSecret:          getEnv("JWT_SECRET", ""),
		JWTAccessTokenTTL:  accessTokenTTL,
		JWTRefreshTokenTTL: refreshTokenTTL,
		CORSAllowedOrigins: strings.Split(getEnv("CORS_ALLOWED_ORIGINS", ""), ","),
		LogLevel:           getEnv("LOG_LEVEL", "info"),
	}
}

// Validate validates the config
func (c *Config) Validate() error {
	if c.AppEnv != "production" && c.AppEnv != "development" {
		return errors.New("invalid app env")
	}

	if c.AppPort == "" {
		return errors.New("invalid app port")
	}

	if c.DBURL == "" {
		return errors.New("database url is required")
	}

	if c.JWTSecret == "" {
		return errors.New("jwt secret is required")
	}

	if len(c.JWTSecret) < 32 {
		return errors.New("jwt secret must be at least 32 characters")
	}

	if len(c.CORSAllowedOrigins) == 0 {
		return errors.New("cors allowed origins is required")
	}

	return nil
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

// getEnv returns the value of the environment variable or the fallback value
func getEnv(key, fallback string) string {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	return val
}
