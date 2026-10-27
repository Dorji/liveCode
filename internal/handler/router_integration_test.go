//go:build integration

// End-to-end HTTP tests that exercise the real router against a real MySQL
// (Redis is optional — the cache degrades gracefully).
//
// Run with:
//
//	make db-up && make test-db && make test-integration
package handler_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"taskmanager/internal/cache"
	"taskmanager/internal/handler"
	"taskmanager/internal/middleware"
	"taskmanager/internal/service"
	"taskmanager/internal/store"
)

// testAPI is a minimal HTTP client for the test server.
type testAPI struct {
	base string
}

func (a testAPI) do(t *testing.T, method, path, token string, body, out any) int {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, a.base+path, rdr)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	if out != nil {
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("unmarshal body: %v", err)
		}
	}
	return resp.StatusCode
}

// newTestServer builds the full router wired to a real MySQL, with a permissive
// rate limiter so the flow is not throttled. It cleans the tables first.
func newTestServer(t *testing.T, rps float64, burst int) *httptest.Server {
	t.Helper()

	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		dsn = "taskuser:taskpass@tcp(127.0.0.1:3306)/taskdb_test?parseTime=true&loc=UTC"
	}

	st, err := store.New(dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := st.Ping(ctx); err != nil {
		_ = st.DB().Close()
		t.Skipf("mysql not reachable (%v), skipping", err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	cleanTables(t, ctx, st.DB())

	// Redis is optional: TaskCache degrades gracefully when unavailable.
	tc := cache.New("localhost:6379", 5*time.Minute)

	authSvc := service.NewAuthService(st, "test-secret", time.Hour)
	h := &handler.Handlers{
		Auth:  authSvc,
		Teams: service.NewTeamService(st),
		Tasks: service.NewTaskService(st, tc),
		Stats: service.NewStatsService(st),
	}
	rl := middleware.NewRateLimiter(rps, burst)
	router := handler.NewRouter(h, authSvc, rl)

	srv := httptest.NewServer(router)
	t.Cleanup(func() {
		srv.Close()
		rl.Stop()
		_ = st.DB().Close()
	})
	return srv
}

func cleanTables(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	for _, table := range []string{"task_comments", "task_history", "tasks", "team_members", "teams", "users"} {
		if _, err := db.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			t.Fatalf("clean %s: %v", table, err)
		}
	}
}

type registerResponse struct {
	Data struct {
		Token string `json:"token"`
		User  struct {
			ID int64 `json:"id"`
		} `json:"user"`
	} `json:"data"`
}

type idResponse struct {
	Data struct {
		ID int64 `json:"id"`
	} `json:"data"`
}

type taskListResponse struct {
	Data struct {
		Tasks []struct {
			ID int64 `json:"id"`
		} `json:"tasks"`
		NextCursor *int64 `json:"next_cursor"`
	} `json:"data"`
}

type statsResponse struct {
	Data struct {
		TotalComments int64 `json:"total_comments"`
	} `json:"data"`
}

func TestHTTPEndToEnd(t *testing.T) {
	srv := newTestServer(t, 1000, 1000)
	api := testAPI{base: srv.URL}

	// Register two users.
	var ra, rb registerResponse
	if st := api.do(t, http.MethodPost, "/api/v1/register", "", map[string]any{"email": "alice@e2e.com", "password": "secret", "name": "Alice"}, &ra); st != http.StatusCreated {
		t.Fatalf("register alice = %d", st)
	}
	if st := api.do(t, http.MethodPost, "/api/v1/register", "", map[string]any{"email": "bob@e2e.com", "password": "secret", "name": "Bob"}, &rb); st != http.StatusCreated {
		t.Fatalf("register bob = %d", st)
	}
	tokenA, tokenB := ra.Data.Token, rb.Data.Token
	bobID := rb.Data.User.ID

	// Unauthenticated request must be rejected.
	if st := api.do(t, http.MethodGet, "/api/v1/teams", "", nil, nil); st != http.StatusUnauthorized {
		t.Fatalf("unauth teams = %d, want 401", st)
	}

	// Create team and invite Bob.
	var team idResponse
	if st := api.do(t, http.MethodPost, "/api/v1/teams", tokenA, map[string]any{"name": "Team"}, &team); st != http.StatusCreated {
		t.Fatalf("create team = %d", st)
	}
	if st := api.do(t, http.MethodPost, fmt.Sprintf("/api/v1/teams/%d/invite", team.Data.ID), tokenA, map[string]any{"user_id": bobID, "role": "member"}, nil); st != http.StatusOK {
		t.Fatalf("invite bob = %d", st)
	}

	// Create three tasks.
	var ids []int64
	for i := 0; i < 3; i++ {
		var tr idResponse
		if st := api.do(t, http.MethodPost, "/api/v1/tasks", tokenA, map[string]any{"team_id": team.Data.ID, "title": fmt.Sprintf("T%d", i)}, &tr); st != http.StatusCreated {
			t.Fatalf("create task %d = %d", i, st)
		}
		ids = append(ids, tr.Data.ID)
	}

	// Cursor pagination: limit=2 -> [max, mid] then [min].
	var p1 taskListResponse
	if st := api.do(t, http.MethodGet, fmt.Sprintf("/api/v1/tasks?team_id=%d&limit=2", team.Data.ID), tokenA, nil, &p1); st != http.StatusOK {
		t.Fatalf("page1 = %d", st)
	}
	if len(p1.Data.Tasks) != 2 || p1.Data.NextCursor == nil {
		t.Fatalf("page1 = %+v", p1.Data)
	}
	var p2 taskListResponse
	if st := api.do(t, http.MethodGet, fmt.Sprintf("/api/v1/tasks?team_id=%d&limit=2&cursor=%d", team.Data.ID, *p1.Data.NextCursor), tokenA, nil, &p2); st != http.StatusOK {
		t.Fatalf("page2 = %d", st)
	}
	if len(p2.Data.Tasks) != 1 || p2.Data.NextCursor != nil {
		t.Fatalf("page2 = %+v", p2.Data)
	}

	got := []int64{p1.Data.Tasks[0].ID, p1.Data.Tasks[1].ID, p2.Data.Tasks[0].ID}
	// Pages must cover exactly the three created tasks, newest first.
	want := []int64{ids[2], ids[1], ids[0]}
	if got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("pagination mismatch: got %v, want %v", got, want)
	}

	// Bob (assignee) can change status only.
	taskID := ids[len(ids)-1]
	if st := api.do(t, http.MethodPut, fmt.Sprintf("/api/v1/tasks/%d", taskID), tokenB, map[string]any{"assignee_id": bobID}, nil); st != http.StatusForbidden {
		t.Fatalf("bob reassign = %d, want 403", st)
	}

	// History and comments.
	if st := api.do(t, http.MethodGet, fmt.Sprintf("/api/v1/tasks/%d/history", taskID), tokenA, nil, nil); st != http.StatusOK {
		t.Fatalf("history = %d", st)
	}
	if st := api.do(t, http.MethodPost, fmt.Sprintf("/api/v1/tasks/%d/comments", taskID), tokenB, map[string]any{"content": "looking"}, nil); st != http.StatusCreated {
		t.Fatalf("comment = %d", st)
	}

	// Stats: owner ok, member forbidden.
	var stats statsResponse
	if st := api.do(t, http.MethodGet, fmt.Sprintf("/api/v1/teams/%d/stats", team.Data.ID), tokenA, nil, &stats); st != http.StatusOK {
		t.Fatalf("stats owner = %d", st)
	}
	if stats.Data.TotalComments != 1 {
		t.Fatalf("total comments = %d, want 1", stats.Data.TotalComments)
	}
	if st := api.do(t, http.MethodGet, fmt.Sprintf("/api/v1/teams/%d/stats", team.Data.ID), tokenB, nil, nil); st != http.StatusForbidden {
		t.Fatalf("stats member = %d, want 403", st)
	}
}

func TestHTTPRateLimit(t *testing.T) {
	srv := newTestServer(t, 1, 1)
	api := testAPI{base: srv.URL}

	if st := api.do(t, http.MethodGet, "/healthz", "", nil, nil); st != http.StatusOK {
		t.Fatalf("first request = %d, want 200", st)
	}
	if st := api.do(t, http.MethodGet, "/healthz", "", nil, nil); st != http.StatusTooManyRequests {
		t.Fatalf("second request = %d, want 429", st)
	}
}
