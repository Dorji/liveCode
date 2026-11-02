package service

import (
	"context"

	"taskmanager/internal/domain"
	"taskmanager/internal/store"
)

type StatsService struct {
	store *store.Store
}

func NewStatsService(st *store.Store) *StatsService {
	return &StatsService{store: st}
}

func (ss *StatsService) TeamStats(ctx context.Context, callerID, teamID int64) (store.TeamStats, error) {
	m, err := ss.store.GetMember(ctx, teamID, callerID)
	if err != nil {
		if err == domain.ErrNotFound {
			return store.TeamStats{}, domain.ErrForbidden
		}
		return store.TeamStats{}, err
	}
	if !canViewStats(m.Role) {
		return store.TeamStats{}, domain.ErrForbidden
	}
	return ss.store.TeamStats(ctx, teamID)
}
