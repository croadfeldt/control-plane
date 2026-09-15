// Package config handles loading and validating service configuration.
package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	Database *DBConfig
	Service  *ServiceConfig
	NATS     *NATSConfig
	Cleanup  *CleanupConfig
}

type CleanupConfig struct {
	Interval   time.Duration `envconfig:"CLEANUP_INTERVAL" default:"1m"`
	MaxRetries int           `envconfig:"CLEANUP_MAX_RETRIES" default:"10"`
	Timeout    time.Duration `envconfig:"CLEANUP_TIMEOUT" default:"10s"`
}

type DBConfig struct {
	Type     string `envconfig:"DB_TYPE" default:"pgsql"`
	Hostname string `envconfig:"DB_HOST" default:"localhost"`
	Port     string `envconfig:"DB_PORT" default:"5432"`
	Name     string `envconfig:"DB_NAME" default:"service-provider"`
	User     string `envconfig:"DB_USER"`
	Password string `envconfig:"DB_PASS"`
}

type ServiceConfig struct {
	Address  string `envconfig:"SVC_ADDRESS" default:":8080"`
	LogLevel string `envconfig:"SVC_LOG_LEVEL" default:"info"`
}

// NATSConfig holds NATS messaging configuration
type NATSConfig struct {
	URL          string `envconfig:"NATS_URL" default:"nats://localhost:4222"`
	Subject      string `envconfig:"NATS_STATUS_SUBJECT" default:"dcm.*"`
	StreamName   string `envconfig:"NATS_STREAM_NAME" default:"dcm-status"`
	ConsumerName string `envconfig:"NATS_CONSUMER_NAME" default:"service-provider-manager"`
}

func Load() (*Config, error) {
	cfg := &Config{}
	if err := envconfig.Process("", cfg); err != nil {
		return nil, err
	}
	if err := validate(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// NormalizeDBType maps DB_TYPE values to a supported backend name.
func NormalizeDBType(dbType string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(dbType)) {
	case "pgsql", "postgres", "postgresql":
		return "pgsql", nil
	case "sqlite":
		return "sqlite", nil
	default:
		return "", fmt.Errorf("unsupported DB_TYPE %q (supported: pgsql, sqlite)", dbType)
	}
}

func validate(cfg *Config) error {
	dbType, err := NormalizeDBType(cfg.Database.Type)
	if err != nil {
		return err
	}
	cfg.Database.Type = dbType

	if dbType != "pgsql" {
		return nil
	}
	if cfg.Database.User == "" {
		return errors.New("DB_USER is required when DB_TYPE is pgsql")
	}
	if cfg.Database.Password == "" {
		return errors.New("DB_PASS is required when DB_TYPE is pgsql")
	}
	return nil
}
