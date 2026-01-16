package logger

import (
	"os"
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	globalLogger *zap.Logger
	once         sync.Once
)

// Initialize sets up the global logger.
// env: "development" or "production"
func Initialize(env string) {
	once.Do(func() {
		var config zap.Config
		if env == "development" {
			config = zap.NewDevelopmentConfig()
			config.Encoding = "json" // Force pure JSON
			config.EncoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder // No colors in JSON
			config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
		} else {
			config = zap.NewProductionConfig()
			config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
		}
		
		config.OutputPaths = []string{"stdout"}
		config.ErrorOutputPaths = []string{"stderr"}

		var err error
		globalLogger, err = config.Build()
		if err != nil {
			// Fallback to basic logger if zap fails
			panic(err)
		}
	})
}

// Get returns the global logger instance.
func Get() *zap.Logger {
	if globalLogger == nil {
		Initialize(os.Getenv("APP_ENV"))
	}
	return globalLogger
}

// Sync flushes any buffered log entries.
func Sync() {
	if globalLogger != nil {
		_ = globalLogger.Sync()
	}
}

// Info logs a message at InfoLevel.
func Info(msg string, fields ...zap.Field) {
	Get().Info(msg, fields...)
}

// Error logs a message at ErrorLevel.
func Error(msg string, fields ...zap.Field) {
	Get().Error(msg, fields...)
}

// Fatal logs a message at FatalLevel.
func Fatal(msg string, fields ...zap.Field) {
	Get().Fatal(msg, fields...)
}

// Debug logs a message at DebugLevel.
func Debug(msg string, fields ...zap.Field) {
	Get().Debug(msg, fields...)
}

// Warn logs a message at WarnLevel.
func Warn(msg string, fields ...zap.Field) {
	Get().Warn(msg, fields...)
}
