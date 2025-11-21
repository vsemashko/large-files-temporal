package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds the cleanup worker configuration
type Config struct {
	// Temporal configuration
	TemporalAddress   string
	TemporalNamespace string

	// S3 configuration
	S3Bucket   string
	S3Region   string
	S3Endpoint string // Optional, for LocalStack/MinIO

	// AWS credentials (prefer IAM roles in production)
	AWSAccessKeyID     string
	AWSSecretAccessKey string

	// Cleanup configuration
	CleanupEnabled          bool
	CleanupSchedule         string // Cron expression
	CleanupGracePeriodDays  int
	CheckArchiveBeforeDelete bool
	MaxWorkflowsPerBatch    int

	// Logging
	LogLevel string
}

// LoadConfig loads configuration from environment variables
func LoadConfig() *Config {
	return &Config{
		TemporalAddress:          getEnv("TEMPORAL_ADDRESS", "localhost:7233"),
		TemporalNamespace:        getEnv("TEMPORAL_NAMESPACE", "default"),
		S3Bucket:                 getEnv("S3_BUCKET", "temporal-large-payloads"),
		S3Region:                 getEnv("S3_REGION", "us-east-1"),
		S3Endpoint:               getEnv("S3_ENDPOINT", ""),
		AWSAccessKeyID:           getEnv("AWS_ACCESS_KEY_ID", ""),
		AWSSecretAccessKey:       getEnv("AWS_SECRET_ACCESS_KEY", ""),
		CleanupEnabled:           getEnvAsBool("CLEANUP_ENABLED", true),
		CleanupSchedule:          getEnv("CLEANUP_SCHEDULE", "0 2 * * *"), // Daily at 2 AM
		CleanupGracePeriodDays:   getEnvAsInt("CLEANUP_GRACE_PERIOD_DAYS", 7),
		CheckArchiveBeforeDelete: getEnvAsBool("CHECK_ARCHIVE_BEFORE_DELETE", true),
		MaxWorkflowsPerBatch:     getEnvAsInt("MAX_WORKFLOWS_PER_BATCH", 100),
		LogLevel:                 getEnv("LOG_LEVEL", "info"),
	}
}

// GetGracePeriod returns the grace period as a duration
func (c *Config) GetGracePeriod() time.Duration {
	return time.Duration(c.CleanupGracePeriodDays) * 24 * time.Hour
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

func getEnvAsBool(key string, defaultValue bool) bool {
	valueStr := getEnv(key, "")
	if value, err := strconv.ParseBool(valueStr); err == nil {
		return value
	}
	return defaultValue
}
