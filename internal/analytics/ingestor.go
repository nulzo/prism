package analytics

import (
	"context"
	"time"

	"github.com/nulzo/model-router-api/internal/store"
	"github.com/nulzo/model-router-api/internal/store/model"
	"go.uber.org/zap"
)

// Ingestor handles the asynchronous persistence of request logs.
type Ingestor interface {
	Log(log *model.RequestLog)
	Start(ctx context.Context)
	Stop()
}

type ingestor struct {
	logger    *zap.Logger
	repo      store.Repository
	logChan   chan *model.RequestLog
	batchSize int
	flushTime time.Duration
}

func NewIngestor(logger *zap.Logger, repo store.Repository) Ingestor {
	return &ingestor{
		logger:    logger,
		repo:      repo,
		logChan:   make(chan *model.RequestLog, 10000),
		batchSize: 50,
		flushTime: 5 * time.Second,
	}
}

func (i *ingestor) Log(log *model.RequestLog) {
	select {
	case i.logChan <- log:
	default:
		i.logger.Warn("Analytics buffer full, dropping log", zap.String("request_id", log.ID))
	}
}

func (i *ingestor) Start(ctx context.Context) {
	go i.worker(ctx)
}

func (i *ingestor) Stop() {
	close(i.logChan)
}

func (i *ingestor) worker(ctx context.Context) {
	batch := make([]*model.RequestLog, 0, i.batchSize)
	ticker := time.NewTicker(i.flushTime)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}

		for _, log := range batch {
			if err := i.repo.Requests().Log(context.Background(), log); err != nil {
				i.logger.Error("Failed to persist request log", zap.String("id", log.ID), zap.Error(err))
			}
		}
		batch = batch[:0]
	}

	for {
		select {
		case log, ok := <-i.logChan:
			if !ok {
				flush()
				return
			}
			batch = append(batch, log)
			if len(batch) >= i.batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-ctx.Done():
			flush()
			return
		}
	}
}
