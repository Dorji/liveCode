package handler

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"taskmanager/internal/domain"
	"taskmanager/internal/middleware"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("write response: %v", err)
	}
}

func writeData(w http.ResponseWriter, status int, data any) {
	writeJSON(w, status, map[string]any{"data": data})
}

func writeError(w http.ResponseWriter, err error) {
	status, msg := mapError(err)
	writeJSON(w, status, map[string]any{"error": msg})
}

func mapError(err error) (int, string) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound, "not found"
	case errors.Is(err, domain.ErrConflict):
		return http.StatusConflict, "conflict"
	case errors.Is(err, domain.ErrForbidden):
		return http.StatusForbidden, "forbidden"
	case errors.Is(err, domain.ErrUnauthorized):
		return http.StatusUnauthorized, "unauthorized"
	case errors.Is(err, domain.ErrBadRequest):
		return http.StatusBadRequest, "bad request"
	case errors.Is(err, domain.ErrEmailTaken):
		return http.StatusConflict, "email already registered"
	case errors.Is(err, domain.ErrAssigneeNotInTeam):
		return http.StatusBadRequest, "assignee must be a member of the team"
	default:
		log.Printf("unhandled error: %v", err)
		return http.StatusInternalServerError, "internal error"
	}
}

func decodeJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(dst)
}

func userID(r *http.Request) int64 {
	id, _ := middleware.UserID(r.Context())
	return id
}

func pathID(r *http.Request, name string) (int64, error) {
	v := r.PathValue(name)
	if v == "" {
		return 0, domain.ErrBadRequest
	}
	id, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, domain.ErrBadRequest
	}
	return id, nil
}

func requiredQueryInt64(r *http.Request, key string) (int64, error) {
	v := r.URL.Query().Get(key)
	if v == "" {
		return 0, domain.ErrBadRequest
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, domain.ErrBadRequest
	}
	return n, nil
}

func optionalQueryInt64(r *http.Request, key string) (*int64, error) {
	v := r.URL.Query().Get(key)
	if v == "" {
		return nil, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return nil, domain.ErrBadRequest
	}
	return &n, nil
}

func optionalQueryStatus(r *http.Request) (*domain.TaskStatus, error) {
	v := r.URL.Query().Get("status")
	if v == "" {
		return nil, nil
	}
	s := domain.TaskStatus(v)
	if !s.Valid() {
		return nil, domain.ErrBadRequest
	}
	return &s, nil
}

func queryLimitOffset(r *http.Request) (int, int, error) {
	limit, offset := 20, 0
	var err error
	if v := r.URL.Query().Get("limit"); v != "" {
		limit, err = strconv.Atoi(v)
		if err != nil {
			return 0, 0, domain.ErrBadRequest
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		offset, err = strconv.Atoi(v)
		if err != nil {
			return 0, 0, domain.ErrBadRequest
		}
	}
	return limit, offset, nil
}
