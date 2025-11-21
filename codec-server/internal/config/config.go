package config

import (
	"os"
	"strconv"
)

// Config holds the codec server configuration
type Config struct {
	// Server configuration
	GRPCPort int
	HTTPPort int

	// Codec configuration
	PayloadSizeThreshold int64
	CompressionEnabled   bool

	// S3 configuration
	S3Bucket   string
	S3Region   string
	S3Endpoint string // Optional, for LocalStack/MinIO

	// AWS credentials (prefer IAM roles in production)
	AWSAccessKeyID     string
	AWSSecretAccessKey string

	// Logging
	LogLevel string
}

// LoadConfig loads configuration from environment variables
func LoadConfig() *Config {
	return &Config{
		GRPCPort:             getEnvAsInt("CODEC_GRPC_PORT", 9090),
		HTTPPort:             getEnvAsInt("CODEC_HTTP_PORT", 8080),
		PayloadSizeThreshold: getEnvAsInt64("PAYLOAD_SIZE_THRESHOLD_BYTES", 2*1024*1024), // 2MB default
		CompressionEnabled:   getEnvAsBool("COMPRESSION_ENABLED", true),
		S3Bucket:             getEnv("S3_BUCKET", "temporal-large-payloads"),
		S3Region:             getEnv("S3_REGION", "us-east-1"),
		S3Endpoint:           getEnv("S3_ENDPOINT", ""),
		AWSAccessKeyID:       getEnv("AWS_ACCESS_KEY_ID", ""),
		AWSSecretAccessKey:   getEnv("AWS_SECRET_ACCESS_KEY", ""),
		LogLevel:             getEnv("LOG_LEVEL", "info"),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	valueStr := getEnv(key, "")
	if value, err := strconv.Atoi(valueStr); err == nil {
		return value
	}
	return defaultValue
}

func getEnvAsInt64(key string, defaultValue int64) int64 {
	valueStr := getEnv(key, "")
	if value, err := strconv.ParseInt(valueStr, 10, 64); err == nil {
		return value
	}
	return defaultValue
}

func getEnvAsBool(key string, defaultValue bool) bool {
	valueStr := getEnv(key, "")
	if value, err := strconv.ParseBool(valueStr); err == nil {
		return value
	}
	return defaultValue
}
