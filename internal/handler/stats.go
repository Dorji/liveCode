package handler

import (
	"net/http"
)

func (h *Handlers) teamStats(w http.ResponseWriter, r *http.Request) {
	teamID, err := pathID(r, "team_id")
	if err != nil {
		writeError(w, err)
		return
	}
	stats, err := h.Stats.TeamStats(r.Context(), userID(r), teamID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, stats)
}
