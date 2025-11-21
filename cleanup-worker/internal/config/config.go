package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
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
	cfg := &Config{
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

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		panic(fmt.Sprintf("Invalid configuration: %v", err))
	}

	return cfg
}

// Validate validates the configuration
func (c *Config) Validate() error {
	// Validate Temporal configuration
	if c.TemporalAddress == "" {
		return fmt.Errorf("Temporal address is required")
	}
	if c.TemporalNamespace == "" {
		return fmt.Errorf("Temporal namespace is required")
	}

	// Validate S3 configuration
	if c.S3Bucket == "" {
		return fmt.Errorf("S3 bucket name is required")
	}
	if c.S3Region == "" {
		return fmt.Errorf("S3 region is required")
	}

	// Validate cleanup configuration
	if c.CleanupGracePeriodDays < 0 {
		return fmt.Errorf("cleanup grace period must be non-negative: %d", c.CleanupGracePeriodDays)
	}
	if c.CleanupGracePeriodDays > 365 {
		return fmt.Errorf("cleanup grace period too large: %d days (maximum: 365)", c.CleanupGracePeriodDays)
	}

	if c.MaxWorkflowsPerBatch < 1 {
		return fmt.Errorf("max workflows per batch must be positive: %d", c.MaxWorkflowsPerBatch)
	}
	if c.MaxWorkflowsPerBatch > 10000 {
		return fmt.Errorf("max workflows per batch too large: %d (maximum: 10000)", c.MaxWorkflowsPerBatch)
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

	return nil
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
