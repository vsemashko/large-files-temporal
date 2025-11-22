package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds the codec server configuration
type Config struct {
	// Server configuration
	GRPCPort int
	HTTPPort int

	// TLS configuration
	TLSEnabled  bool
	TLSCertFile string
	TLSKeyFile  string

	// Authentication
	APIKey string

	// Rate limiting
	RateLimitRPS   int // Requests per second
	RateLimitBurst int // Burst size

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
	cfg := &Config{
		GRPCPort:             getEnvAsInt("CODEC_GRPC_PORT", 9090),
		HTTPPort:             getEnvAsInt("CODEC_HTTP_PORT", 8080),
		TLSEnabled:           getEnvAsBool("CODEC_TLS_ENABLED", false),
		TLSCertFile:          getEnv("CODEC_TLS_CERT_FILE", ""),
		TLSKeyFile:           getEnv("CODEC_TLS_KEY_FILE", ""),
		APIKey:               getEnv("CODEC_API_KEY", ""),
		RateLimitRPS:         getEnvAsInt("RATE_LIMIT_RPS", 100),   // 100 requests per second default
		RateLimitBurst:       getEnvAsInt("RATE_LIMIT_BURST", 200), // 200 burst default
		PayloadSizeThreshold: getEnvAsInt64("PAYLOAD_SIZE_THRESHOLD_BYTES", 2*1024*1024), // 2MB default
		CompressionEnabled:   getEnvAsBool("COMPRESSION_ENABLED", true),
		S3Bucket:             getEnv("S3_BUCKET", "temporal-large-payloads"),
		S3Region:             getEnv("S3_REGION", "us-east-1"),
		S3Endpoint:           getEnv("S3_ENDPOINT", ""),
		AWSAccessKeyID:       getEnv("AWS_ACCESS_KEY_ID", ""),
		AWSSecretAccessKey:   getEnv("AWS_SECRET_ACCESS_KEY", ""),
		LogLevel:             getEnv("LOG_LEVEL", "info"),
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		panic(fmt.Sprintf("Invalid configuration: %v", err))
	}

	return cfg
}

// Validate validates the configuration
func (c *Config) Validate() error {
	// Validate ports
	if c.GRPCPort < 1 || c.GRPCPort > 65535 {
		return fmt.Errorf("invalid GRPC port: %d (must be 1-65535)", c.GRPCPort)
	}
	if c.HTTPPort < 1 || c.HTTPPort > 65535 {
		return fmt.Errorf("invalid HTTP port: %d (must be 1-65535)", c.HTTPPort)
	}
	if c.GRPCPort == c.HTTPPort {
		return fmt.Errorf("GRPC and HTTP ports must be different")
	}

	// Validate payload threshold
	const minThreshold = 1024           // 1KB minimum
	const maxThreshold = 100 * 1024 * 1024 // 100MB maximum
	if c.PayloadSizeThreshold < minThreshold {
		return fmt.Errorf("payload threshold too small: %d bytes (minimum: %d)", c.PayloadSizeThreshold, minThreshold)
	}
	if c.PayloadSizeThreshold > maxThreshold {
		return fmt.Errorf("payload threshold too large: %d bytes (maximum: %d)", c.PayloadSizeThreshold, maxThreshold)
	}

	// Validate S3 configuration
	if c.S3Bucket == "" {
		return fmt.Errorf("S3 bucket name is required")
	}
	if c.S3Region == "" {
		return fmt.Errorf("S3 region is required")
	}

	// Validate log level
	validLogLevels := []string{"debug", "info", "warn", "error"}
	logLevel := strings.ToLower(c.LogLevel)
	valid := false
	for _, level := range validLogLevels {
		if logLevel == level {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("invalid log level: %s (must be one of: %v)", c.LogLevel, validLogLevels)
	}

	// Validate TLS configuration
	if c.TLSEnabled {
		if c.TLSCertFile == "" {
			return fmt.Errorf("TLS cert file is required when TLS is enabled")
		}
		if c.TLSKeyFile == "" {
			return fmt.Errorf("TLS key file is required when TLS is enabled")
		}
	}

	// Validate rate limit configuration
	if c.RateLimitRPS < 0 {
		return fmt.Errorf("rate limit RPS cannot be negative: %d", c.RateLimitRPS)
	}
	if c.RateLimitRPS > 10000 {
		return fmt.Errorf("rate limit RPS too high: %d (maximum: 10000)", c.RateLimitRPS)
	}
	if c.RateLimitBurst < 1 {
		return fmt.Errorf("rate limit burst must be at least 1: %d", c.RateLimitBurst)
	}
	if c.RateLimitBurst > 20000 {
		return fmt.Errorf("rate limit burst too high: %d (maximum: 20000)", c.RateLimitBurst)
	}

	return nil
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
