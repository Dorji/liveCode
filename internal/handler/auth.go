package handler

import (
	"net/http"

	"taskmanager/internal/domain"
)

func (h *Handlers) register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Name     string `json:"name"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, domain.ErrBadRequest)
		return
	}

	u, err := h.Auth.Register(r.Context(), req.Email, req.Password, req.Name)
	if err != nil {
		writeError(w, err)
		return
	}
	token, err := h.Auth.TokenFor(u)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusCreated, map[string]any{"user": u, "token": token})
}

func (h *Handlers) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, domain.ErrBadRequest)
		return
	}

	token, err := h.Auth.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, map[string]any{"token": token})
}
