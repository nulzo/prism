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

type Format string

const (
	Console Format = "console"
)

var (
	globalLogger *zap.Logger
	mu           sync.RWMutex
	once         sync.Once
)

// DefaultConfig returns a sane default configuration based on environment variables.
func DefaultConfig() Config {
	return Config{
		Level:       getEnv("LOG_LEVEL", "debug"),
		Format:      getEnv("LOG_FORMAT", "console"), // options: json, console
		EnableColor: shouldEnableColor(),
	}
}

// New creates a new logger instance.
func New(cfg Config) (*zap.Logger, error) {
	once.Do(func() {
		err := zap.RegisterEncoder("colored-console", func(zcfg zapcore.EncoderConfig) (zapcore.Encoder, error) {
			return NewColoredConsoleEncoder(zcfg), nil
		})
		if err != nil {
			// this might panic if registered twice without check, but once prevents it.
			panic(err)
		}
	})

	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.TimeKey = "timestamp"
	encoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout("15:04:05")
	encoderConfig.EncodeCaller = zapcore.ShortCallerEncoder

	// Colorize the LEVEL (INFO, DEBUG) specifically
	if cfg.Format == string(Console) && cfg.EnableColor {
		encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
		encoderConfig.EncodeCaller = nil
	} else {
		encoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
	}

	// Select the encoding based on config
	encoding := cfg.Format
	if cfg.Format == string(Console) && cfg.EnableColor {
		encoding = "colored-console"
	}

	zapConfig := zap.Config{
		Level:             zap.NewAtomicLevelAt(parseLevel(cfg.Level)),
		Development:       cfg.Format == string(Console),
		Encoding:          encoding,
		EncoderConfig:     encoderConfig,
		OutputPaths:       []string{"stdout"},
		ErrorOutputPaths:  []string{"stderr"},
		DisableStacktrace: false,
	}

	return zapConfig.Build(zap.AddCallerSkip(1), zap.AddStacktrace(zapcore.PanicLevel))
}

// SetGlobal sets the global logger instance.
func SetGlobal(l *zap.Logger) {
	mu.Lock()
	defer mu.Unlock()
	globalLogger = l
}

// Get returns the global logger. Initializes with defaults if not already set.
func Get() *zap.Logger {
	mu.RLock()
	if globalLogger != nil {
		defer mu.RUnlock()
		return globalLogger
	}
	mu.RUnlock()

	// Initialize default if nil
	l, _ := New(DefaultConfig())
	SetGlobal(l)
	return l
}

// With creates a child logger and adds structured context to it.
func With(fields ...zap.Field) *zap.Logger {
	return Get().With(fields...)
}

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
	mu.RLock()
	l := globalLogger
	mu.RUnlock()
	if l != nil {
		_ = l.Sync()
	}
}

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
	if _, noColor := os.LookupEnv("NO_COLOR"); noColor {
		return false
	}
	if val := os.Getenv("LOG_COLOR"); val != "" {
		return val == "true" || val == "1"
	}
	return true
}
