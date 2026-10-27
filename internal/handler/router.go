package handler

import (
	"net/http"

	"taskmanager/internal/middleware"
	"taskmanager/internal/service"
)

// Handlers groups the service dependencies used by the HTTP layer.
type Handlers struct {
	Auth  *service.AuthService
	Teams *service.TeamService
	Tasks *service.TaskService
	Stats *service.StatsService
}

func NewRouter(h *Handlers, parser middleware.TokenParser, rl *middleware.RateLimiter) http.Handler {
	mux := http.NewServeMux()

	// Public.
	mux.HandleFunc("POST /api/v1/register", h.register)
	mux.HandleFunc("POST /api/v1/login", h.login)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})

	// Protected.
	auth := middleware.Auth(parser)
	mux.Handle("POST /api/v1/teams", auth(http.HandlerFunc(h.createTeam)))
	mux.Handle("GET /api/v1/teams", auth(http.HandlerFunc(h.listTeams)))
	mux.Handle("POST /api/v1/teams/{id}/invite", auth(http.HandlerFunc(h.invite)))
	mux.Handle("POST /api/v1/tasks", auth(http.HandlerFunc(h.createTask)))
	mux.Handle("GET /api/v1/tasks", auth(http.HandlerFunc(h.listTasks)))
	mux.Handle("PUT /api/v1/tasks/{id}", auth(http.HandlerFunc(h.updateTask)))
	mux.Handle("GET /api/v1/tasks/{id}/history", auth(http.HandlerFunc(h.taskHistory)))
	mux.Handle("POST /api/v1/tasks/{id}/comments", auth(http.HandlerFunc(h.addComment)))
	mux.Handle("GET /api/v1/tasks/{id}/comments", auth(http.HandlerFunc(h.listComments)))
	mux.Handle("GET /api/v1/teams/{team_id}/stats", auth(http.HandlerFunc(h.teamStats)))

	// Swagger / OpenAPI.
	mux.HandleFunc("GET /openapi.yaml", serveOpenAPI)
	mux.HandleFunc("GET /swagger", serveSwaggerUI)

	// request id + structured request log -> rate limit -> routes
	return middleware.RequestID(middleware.RequestLogger(rl.Middleware(mux)))
}
