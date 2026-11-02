package service

import (
	"context"

	"taskmanager/internal/domain"
	"taskmanager/internal/store"
)

type TeamService struct {
	store *store.Store
}

func NewTeamService(st *store.Store) *TeamService {
	return &TeamService{store: st}
}

func (ts *TeamService) Create(ctx context.Context, userID int64, name string) (domain.Team, error) {
	if name == "" {
		return domain.Team{}, domain.ErrBadRequest
	}
	return ts.store.CreateTeamWithOwner(ctx, name, userID)
}

func (ts *TeamService) List(ctx context.Context, userID int64) ([]domain.Team, error) {
	return ts.store.ListTeamsForUser(ctx, userID)
}

type InviteInput struct {
	UserID int64  `json:"user_id"`
	Role   string `json:"role"`
}

func (ts *TeamService) Invite(ctx context.Context, callerID, teamID int64, in InviteInput) error {
	role := domain.Role(in.Role)
	if !role.Valid() {
		return domain.ErrBadRequest
	}
	if role == domain.RoleOwner {
		return domain.ErrForbidden
	}

	caller, err := ts.store.GetMember(ctx, teamID, callerID)
	if err != nil {
		if err == domain.ErrNotFound {
			return domain.ErrForbidden
		}
		return err
	}
	if !canInvite(caller.Role) {
		return domain.ErrForbidden
	}

	if _, err := ts.store.GetUserByID(ctx, in.UserID); err != nil {
		return err
	}

	// The team creator stays owner; nobody (not even the owner) may demote them.
	team, err := ts.store.GetTeam(ctx, teamID)
	if err != nil {
		return err
	}
	if team.CreatedBy == in.UserID {
		return domain.ErrForbidden
	}

	return ts.store.UpsertMemberRole(ctx, teamID, in.UserID, role)
}
