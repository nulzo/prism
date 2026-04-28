package v1

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nulzo/model-router-api/internal/gateway"
	"github.com/nulzo/model-router-api/internal/server/validator"
	"github.com/nulzo/model-router-api/pkg/api"
)

type SpeechHandler struct {
	service   gateway.Service
	validator *validator.Validator
}

func NewSpeechHandler(service gateway.Service, v *validator.Validator) *SpeechHandler {
	return &SpeechHandler{
		service:   service,
		validator: v,
	}
}

func (h *SpeechHandler) CreateSpeech(c *gin.Context) {
	var req api.SpeechRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(api.ValidationError(h.validator.ParseError(err)))
		return
	}

	genID := c.GetHeader("X-Generation-Id")
	if genID == "" {
		genID = uuid.NewString()
	}

	wrote := false
	err := h.service.StreamSpeech(
		gateway.WithGenerationID(c.Request.Context(), genID),
		&req,
		func(contentType string, chunk []byte) error {
			if !wrote {
				if contentType == "" {
					contentType = "application/octet-stream"
				}
				c.Header("Content-Type", contentType)
				c.Header("Cache-Control", "no-store")
				c.Header("X-Accel-Buffering", "no")
				c.Header("X-Generation-Id", genID)
				c.Status(http.StatusOK)
				wrote = true
			}
			if _, err := c.Writer.Write(chunk); err != nil {
				return err
			}
			c.Writer.Flush()
			return nil
		},
	)
	if err != nil {
		if !wrote {
			_ = c.Error(err)
			return
		}
		_ = c.Error(err)
		return
	}
	if !wrote {
		c.Header("Content-Type", "application/octet-stream")
		c.Header("X-Generation-Id", genID)
		c.Status(http.StatusOK)
		_, _ = io.WriteString(c.Writer, "")
	}
}
