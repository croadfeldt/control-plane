package store

import (
	"bytes"
	"log"
	"log/slog"
	"strings"
	"testing"

	"github.com/dcm-project/control-plane/internal/sp/store/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestGormLogLevelFromString(t *testing.T) {
	tests := []struct {
		level   string
		gormLvl logger.LogLevel
		slogLvl slog.Level
	}{
		{"debug", logger.Info, slog.LevelDebug},
		{"info", logger.Info, slog.LevelInfo},
		{"warn", logger.Warn, slog.LevelWarn},
		{"warning", logger.Warn, slog.LevelWarn},
		{"error", logger.Error, slog.LevelError},
		{"unknown", logger.Warn, slog.LevelWarn},
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			gotGorm, gotSlog := gormLogLevelFromString(tt.level)
			if gotGorm != tt.gormLvl {
				t.Fatalf("gorm level: got %v, want %v", gotGorm, tt.gormLvl)
			}
			if gotSlog != tt.slogLvl {
				t.Fatalf("slog level: got %v, want %v", gotSlog, tt.slogLvl)
			}
		})
	}
}

func TestParameterizedQueriesRedactsBoundValues(t *testing.T) {
	var buf bytes.Buffer

	// Use the same logger config as production InitDB so the test
	// fails if ParameterizedQueries is removed from the shared config.
	cfg := gormLoggerConfig(logger.Info)
	gormLogger := logger.New(log.New(&buf, "", 0), cfg)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: gormLogger,
	})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.ServiceTypeInstance{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	const marker = "SENSITIVE-SECRET-MARKER"

	instance := model.ServiceTypeInstance{
		ID:           "test-redact-instance",
		Status:       "pending",
		InstanceName: "redact-test",
		Spec:         map[string]any{"cpu": 2},
	}
	if err := db.Create(&instance).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	buf.Reset()

	result := db.Model(&model.ServiceTypeInstance{}).
		Where("id = ?", instance.ID).
		Select("status", "status_message", "output_spec").
		Updates(&model.ServiceTypeInstance{
			Status:     "running",
			OutputSpec: map[string]any{"connection_string": marker},
		})
	if result.Error != nil {
		t.Fatalf("update failed: %v", result.Error)
	}
	if result.RowsAffected != 1 {
		t.Fatalf("expected 1 row affected, got %d", result.RowsAffected)
	}

	logged := buf.String()

	if strings.Contains(logged, marker) {
		t.Fatalf("log output contains secret marker %q:\n%s", marker, logged)
	}
	if !strings.Contains(logged, "output_spec") {
		t.Fatalf("log output missing query shape (expected column name):\n%s", logged)
	}
}
