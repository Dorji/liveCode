//go:build integration

// Integration test for the SQL report (TeamStats). It requires a running
// MySQL. Run with:
//
//	TEST_MYSQL_DSN="taskuser:taskpass@tcp(127.0.0.1:3306)/taskdb_test?parseTime=true&loc=UTC" \
//	  go test -tags integration ./internal/store/ -run TestTeamStats -v
//
// The test skips itself when the database is unreachable.
package store_test

import (
	"context"
	"database/sql"
	"math"
	"os"
	"testing"
	"time"

	"taskmanager/internal/domain"
	"taskmanager/internal/store"
)

func TestTeamStats(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		dsn = "taskuser:taskpass@tcp(127.0.0.1:3306)/taskdb_test?parseTime=true&loc=UTC"
	}

	st, err := store.New(dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.DB().Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := st.Ping(ctx); err != nil {
		t.Skipf("mysql not reachable (%v), skipping integration test", err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	cleanTables(t, ctx, st.DB())

	now := time.Now().UTC()
	seedUser(t, ctx, st.DB(), "alice@example.com", "Alice") // id 1
	seedUser(t, ctx, st.DB(), "bob@example.com", "Bob")     // id 2
	seedUser(t, ctx, st.DB(), "carol@example.com", "Carol") // id 3

	teamID := seedTeam(t, ctx, st.DB(), "team", 1)
	seedMember(t, ctx, st.DB(), teamID, 1, "owner")
	seedMember(t, ctx, st.DB(), teamID, 2, "member")
	seedMember(t, ctx, st.DB(), teamID, 3, "member")

	// Bob closes 2 tasks within the last 30 days, Carol 1; Bob also has an old
	// closed task (outside 30 days). This shapes both top_assignees and avg close.
	t1 := seedTask(t, ctx, st.DB(), teamID, "t1", 1, idp(2), now.Add(-20*24*time.Hour), now.Add(-10*24*time.Hour), "done")
	t2 := seedTask(t, ctx, st.DB(), teamID, "t2", 1, idp(2), now.Add(-50*24*time.Hour), now.Add(-40*24*time.Hour), "done")
	_ = seedTask(t, ctx, st.DB(), teamID, "t3", 1, idp(3), now.Add(-15*24*time.Hour), now.Add(-5*24*time.Hour), "done")
	_ = seedTask(t, ctx, st.DB(), teamID, "t4", 2, idp(2), now.Add(-3*24*time.Hour), now.Add(-2*24*time.Hour), "done")
	t5 := seedTask(t, ctx, st.DB(), teamID, "t5", 2, idp(3), now.Add(-1*24*time.Hour), time.Time{}, "in_progress")
	_ = seedTask(t, ctx, st.DB(), teamID, "t6", 1, nil, now.Add(-1*24*time.Hour), time.Time{}, "todo")

	seedComment(t, ctx, st.DB(), t1, 1, "a")
	seedComment(t, ctx, st.DB(), t1, 2, "b")
	seedComment(t, ctx, st.DB(), t2, 2, "c")
	seedComment(t, ctx, st.DB(), t5, 3, "d")

	stats, err := st.TeamStats(ctx, teamID)
	if err != nil {
		t.Fatalf("TeamStats: %v", err)
	}

	// Status counts: done=4, in_progress=1, todo=1.
	if stats.StatusCounts[domain.StatusDone] != 4 ||
		stats.StatusCounts[domain.StatusInProgress] != 1 ||
		stats.StatusCounts[domain.StatusTodo] != 1 {
		t.Fatalf("status counts = %+v", stats.StatusCounts)
	}

	// Top assignees by closed tasks within 30 days: Bob=2 (t1, t4), Carol=1 (t3).
	if len(stats.TopAssignees) != 2 {
		t.Fatalf("expected 2 top assignees, got %+v", stats.TopAssignees)
	}
	if stats.TopAssignees[0].AssigneeID != 2 || stats.TopAssignees[0].ClosedCount != 2 {
		t.Fatalf("top[0] = %+v, want Bob with 2", stats.TopAssignees[0])
	}
	if stats.TopAssignees[1].AssigneeID != 3 || stats.TopAssignees[1].ClosedCount != 1 {
		t.Fatalf("top[1] = %+v, want Carol with 1", stats.TopAssignees[1])
	}

	// Avg close (days): t1=10, t2=10, t3=10, t4=1 -> 31/4 = 7.75 days.
	wantAvg := 31.0 * 24 * 3600 / 4.0
	if math.Abs(stats.AvgCloseSeconds-wantAvg) > 1.0 {
		t.Fatalf("avg close = %f, want ~%f", stats.AvgCloseSeconds, wantAvg)
	}

	if stats.TotalComments != 4 {
		t.Fatalf("total comments = %d, want 4", stats.TotalComments)
	}
}

func idp(v int64) *int64 { return &v }

func seedUser(t *testing.T, ctx context.Context, db *sql.DB, email, name string) {
	t.Helper()
	if _, err := db.ExecContext(ctx,
		`INSERT INTO users (email, password_hash, name) VALUES (?, 'x', ?)`, email, name); err != nil {
		t.Fatalf("seed user: %v", err)
	}
}

func seedTeam(t *testing.T, ctx context.Context, db *sql.DB, name string, createdBy int64) int64 {
	t.Helper()
	res, err := db.ExecContext(ctx, `INSERT INTO teams (name, created_by) VALUES (?, ?)`, name, createdBy)
	if err != nil {
		t.Fatalf("seed team: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func seedMember(t *testing.T, ctx context.Context, db *sql.DB, teamID, userID int64, role string) {
	t.Helper()
	if _, err := db.ExecContext(ctx,
		`INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, ?)`, teamID, userID, role); err != nil {
		t.Fatalf("seed member: %v", err)
	}
}

func seedTask(t *testing.T, ctx context.Context, db *sql.DB, teamID int64, title string, createdBy int64, assignee *int64, createdAt, closedAt time.Time, status string) int64 {
	t.Helper()
	var closed any
	if closedAt.IsZero() {
		closed = nil
	} else {
		closed = closedAt
	}
	res, err := db.ExecContext(ctx,
		`INSERT INTO tasks (team_id, title, status, created_by, assignee_id, created_at, closed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		teamID, title, status, createdBy, assignee, createdAt, closed)
	if err != nil {
		t.Fatalf("seed task: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func seedComment(t *testing.T, ctx context.Context, db *sql.DB, taskID, userID int64, content string) {
	t.Helper()
	if _, err := db.ExecContext(ctx,
		`INSERT INTO task_comments (task_id, user_id, content) VALUES (?, ?, ?)`, taskID, userID, content); err != nil {
		t.Fatalf("seed comment: %v", err)
	}
}

func cleanTables(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	for _, table := range []string{"task_comments", "task_history", "tasks", "team_members", "teams", "users"} {
		if _, err := db.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			t.Fatalf("clean %s: %v", table, err)
		}
	}
}
