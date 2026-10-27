package store

import (
	"context"
	"database/sql"
	"errors"

	"taskmanager/internal/domain"
)

// CreateTeamWithOwner creates a team and its creator membership atomically:
// a team must never exist without its owner.
func (s *Store) CreateTeamWithOwner(ctx context.Context, name string, userID int64) (domain.Team, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Team{}, err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`INSERT INTO teams (name, created_by) VALUES (?, ?)`, name, userID)
	if err != nil {
		return domain.Team{}, err
	}
	teamID, err := res.LastInsertId()
	if err != nil {
		return domain.Team{}, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, ?)`,
		teamID, userID, domain.RoleOwner); err != nil {
		return domain.Team{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Team{}, err
	}
	return s.GetTeam(ctx, teamID)
}

func (s *Store) GetTeam(ctx context.Context, id int64) (domain.Team, error) {
	var t domain.Team
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, created_by, created_at FROM teams WHERE id = ?`, id).
		Scan(&t.ID, &t.Name, &t.CreatedBy, &t.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Team{}, domain.ErrNotFound
	}
	return t, err
}

func (s *Store) ListTeamsForUser(ctx context.Context, userID int64) ([]domain.Team, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT t.id, t.name, t.created_by, t.created_at
		 FROM teams t
		 JOIN team_members tm ON tm.team_id = t.id
		 WHERE tm.user_id = ?
		 ORDER BY t.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	teams := []domain.Team{}
	for rows.Next() {
		var t domain.Team
		if err := rows.Scan(&t.ID, &t.Name, &t.CreatedBy, &t.CreatedAt); err != nil {
			return nil, err
		}
		teams = append(teams, t)
	}
	return teams, rows.Err()
}

// GetMember returns the membership of user in team, or domain.ErrNotFound.
func (s *Store) GetMember(ctx context.Context, teamID, userID int64) (domain.TeamMember, error) {
	var m domain.TeamMember
	var role string
	err := s.db.QueryRowContext(ctx,
		`SELECT team_id, user_id, role FROM team_members WHERE team_id = ? AND user_id = ?`,
		teamID, userID).Scan(&m.TeamID, &m.UserID, &role)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.TeamMember{}, domain.ErrNotFound
	}
	m.Role = domain.Role(role)
	return m, err
}

// UpsertMemberRole adds a member or, if already present, changes their role.
func (s *Store) UpsertMemberRole(ctx context.Context, teamID, userID int64, role domain.Role) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, ?)
		 ON DUPLICATE KEY UPDATE role = VALUES(role)`,
		teamID, userID, role)
	return err
}
