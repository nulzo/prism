package logger

import (
	"strings"

	"github.com/nulzo/model-router-api/internal/cli"
	"go.uber.org/zap/buffer"
	"go.uber.org/zap/zapcore"
)

// coloredConsoleEncoder wraps zap's standard console encoder to add syntax highlighting to JSON blobs
type coloredConsoleEncoder struct {
	zapcore.Encoder
}

func NewColoredConsoleEncoder(cfg zapcore.EncoderConfig) zapcore.Encoder {
	// We use the standard Console Encoder for the heavy lifting (time, level, caller)
	return &coloredConsoleEncoder{
		Encoder: zapcore.NewConsoleEncoder(cfg),
	}
}

// Clone is required to implement the Encoder interface
func (c *coloredConsoleEncoder) Clone() zapcore.Encoder {
	return &coloredConsoleEncoder{
		Encoder: c.Encoder.Clone(),
	}
}

// EncodeEntry is where the magic happens.
func (c *coloredConsoleEncoder) EncodeEntry(ent zapcore.Entry, fields []zapcore.Field) (*buffer.Buffer, error) {
	// 1. Let the standard encoder format the line (Time, Level, Msg, JSON Fields)
	buf, err := c.Encoder.EncodeEntry(ent, fields)
	if err != nil {
		return nil, err
	}

	// 2. Convert buffer to string to manipulate it
	logLine := buf.String()

	// Zap Console Encoder usually separates the Metadata from the JSON fields with a tab
	// Example: "TIMESTAMP INFO MSG\t{json...}"
	// We look for the first occurrence of '{' after the standard headers.
	// This is a heuristic: we assume the last part of the line starting with { is the JSON blob.

	splitIdx := strings.Index(logLine, "\t{")

	if splitIdx != -1 {
		// We found a JSON blob separator
		metaPart := logLine[:splitIdx+1] // Include the tab
		jsonPart := logLine[splitIdx+1:] // The JSON blob (including newline)

		// 3. Highlight the JSON part
		prettyJSON := cli.HighlightJSON(jsonPart)

		// 4. Reconstruct the buffer
		newBuf := buffer.NewPool().Get()
		newBuf.AppendString(metaPart)
		newBuf.AppendString(prettyJSON)

		// Free the old buffer
		buf.Free()

		return newBuf, nil
	}

	// If no JSON detected, return original buffer
	return buf, nil
}
