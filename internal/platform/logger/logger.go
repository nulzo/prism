package logger

import (
	"os"
	"strings"
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Config defines the configuration for the logger.
type Config struct {
	Level       string // debug, info, warn, error
	Format      string // json, console
	EnableColor bool   // true to enable colors (only in console mode)
}

var (
	globalLogger *zap.Logger
	atom         zap.AtomicLevel
	once         sync.Once
)

// DefaultConfig returns a sane default configuration based on environment variables.
func DefaultConfig() Config {
	return Config{
		Level:       getEnv("LOG_LEVEL", "info"),
		Format:      getEnv("LOG_FORMAT", "console"), // options: json, console
		EnableColor: shouldEnableColor(),
	}
}

// Initialize sets up the global logger using the provided configuration.
func Initialize(cfg Config) {
	once.Do(func() {
		// Create the encoder config
		encoderConfig := zap.NewProductionEncoderConfig()

		// Standardize Time Format
		encoderConfig.TimeKey = "timestamp"
		encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

		// Configure Level Encoding
		if cfg.Format == "console" && cfg.EnableColor {
			encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
		} else {
			encoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
		}

		// Configure Callers (shorter path for console, full for json usually)
		if cfg.Format == "console" {
			encoderConfig.EncodeCaller = zapcore.ShortCallerEncoder
			encoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout("15:04:05")
		}

		// Build the Zap Config
		zapConfig := zap.Config{
			Level:             zap.NewAtomicLevelAt(parseLevel(cfg.Level)),
			Development:       false,
			Sampling:          nil, // Consider sampling for high-load production
			Encoding:          cfg.Format,
			EncoderConfig:     encoderConfig,
			OutputPaths:       []string{"stdout"},
			ErrorOutputPaths:  []string{"stderr"},
			DisableStacktrace: cfg.Level != "debug" && cfg.Level != "error",
		}

		// Build the logger
		var err error
		globalLogger, err = zapConfig.Build(zap.AddCallerSkip(1))
		if err != nil {
			panic("failed to initialize logger: " + err.Error())
		}

		// Capture the atomic level so we can change it dynamically at runtime if needed
		atom = zapConfig.Level
	})
}

// Get returns the global logger. Initializes with defaults if not already set.
func Get() *zap.Logger {
	if globalLogger == nil {
		Initialize(DefaultConfig())
	}
	return globalLogger
}

// With creates a child logger and adds structured context to it.
func With(fields ...zap.Field) *zap.Logger {
	return Get().With(fields...)
}

// --- Wrapper Functions ---

func Info(msg string, fields ...zap.Field) {
	Get().Info(msg, fields...)
}

func Error(msg string, fields ...zap.Field) {
	Get().Error(msg, fields...)
}

func Fatal(msg string, fields ...zap.Field) {
	Get().Fatal(msg, fields...)
}

func Debug(msg string, fields ...zap.Field) {
	Get().Debug(msg, fields...)
}

func Warn(msg string, fields ...zap.Field) {
	Get().Warn(msg, fields...)
}

func Sync() {
	if globalLogger != nil {
		_ = globalLogger.Sync()
	}
}

// --- Helpers ---

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return strings.ToLower(value)
	}
	return fallback
}

func parseLevel(lvl string) zapcore.Level {
	switch strings.ToLower(lvl) {
	case "debug":
		return zapcore.DebugLevel
	case "info":
		return zapcore.InfoLevel
	case "warn":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	case "fatal":
		return zapcore.FatalLevel
	default:
		return zapcore.InfoLevel
	}
}

// shouldEnableColor checks NO_COLOR (standard) and LOG_COLOR
func shouldEnableColor() bool {
	// 1. Check strict NO_COLOR standard (https://no-color.org/)
	if _, noColor := os.LookupEnv("NO_COLOR"); noColor {
		return false
	}
	// 2. Allow explicit force enable/disable via LOG_COLOR
	if val := os.Getenv("LOG_COLOR"); val != "" {
		return val == "true" || val == "1"
	}
	// 3. Default: Enable color if terminal (simplified approach, assumes yes for dev experience)
	return true
}
