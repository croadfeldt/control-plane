package store

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/dcm-project/control-plane/internal/sp/config"
	"github.com/dcm-project/control-plane/internal/sp/store/model"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func InitDB(cfg *config.Config) (*gorm.DB, error) {
	var dialector gorm.Dialector

	if cfg.Database.Type == "pgsql" {
		dsn := fmt.Sprintf("host=%s user=%s password=%s port=%s dbname=%s",
			cfg.Database.Hostname,
			cfg.Database.User,
			cfg.Database.Password,
			cfg.Database.Port,
			cfg.Database.Name,
		)
		dialector = postgres.Open(dsn)
	} else {
		dialector = sqlite.Open(cfg.Database.Name)
	}

	logLevel := "info"
	if cfg.Service != nil {
		logLevel = cfg.Service.LogLevel
	}
	gormLogLevel, slogBridgeLevel := gormLogLevelFromString(logLevel)

	gormLogger := logger.New(
		slog.NewLogLogger(slog.Default().Handler(), slogBridgeLevel),
		logger.Config{
			SlowThreshold:             time.Second,
			LogLevel:                  gormLogLevel,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		},
	)

	db, err := gorm.Open(dialector, &gorm.Config{
		Logger:         gormLogger,
		TranslateError: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying db: %w", err)
	}
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)

	slog.Info("Database connection established", "type", cfg.Database.Type)

	if err := db.AutoMigrate(&model.ServiceTypeInstance{}); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	slog.Info("Database schema migrated")

	return db, nil
}

// gormLogLevelFromString maps the application log level string to GORM and slog levels.
func gormLogLevelFromString(level string) (logger.LogLevel, slog.Level) {
	switch strings.ToLower(level) {
	case "debug":
		return logger.Info, slog.LevelDebug
	case "info":
		return logger.Info, slog.LevelInfo
	case "warn", "warning":
		return logger.Warn, slog.LevelWarn
	case "error":
		return logger.Error, slog.LevelError
	default:
		return logger.Warn, slog.LevelWarn
	}
}
