package handler

import (
	"net/http"

	"taskmanager/internal/domain"
	"taskmanager/internal/service"
)

func (h *Handlers) createTeam(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, domain.ErrBadRequest)
		return
	}

	team, err := h.Teams.Create(r.Context(), userID(r), req.Name)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusCreated, team)
}

func (h *Handlers) listTeams(w http.ResponseWriter, r *http.Request) {
	teams, err := h.Teams.List(r.Context(), userID(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, teams)
}

func (h *Handlers) invite(w http.ResponseWriter, r *http.Request) {
	teamID, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	var in service.InviteInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, domain.ErrBadRequest)
		return
	}

	if err := h.Teams.Invite(r.Context(), userID(r), teamID, in); err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, map[string]any{"ok": true})
}
