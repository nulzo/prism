package analytics

import (
	"context"

	"github.com/nulzo/model-router-api/internal/store"
	"github.com/nulzo/model-router-api/internal/store/model"
)

type Service interface {
	GetUsageOverview(ctx context.Context, days int) ([]model.DailyStats, error)
}

type service struct {
	repo store.Repository
}

func NewService(repo store.Repository) Service {
	return &service{
		repo: repo,
	}
}

func (s *service) GetUsageOverview(ctx context.Context, days int) ([]model.DailyStats, error) {
	// TODO: caching or other processing?
	if days <= 0 {
		days = 7 // default to last week
	}
	return s.repo.Requests().GetDailyStats(ctx, days)
}
