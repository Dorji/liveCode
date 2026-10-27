package handler

import (
	"net/http"

	"taskmanager/internal/domain"
	"taskmanager/internal/service"
)

func (h *Handlers) createTask(w http.ResponseWriter, r *http.Request) {
	var in service.CreateTaskInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, domain.ErrBadRequest)
		return
	}

	task, err := h.Tasks.Create(r.Context(), userID(r), in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusCreated, task)
}

func (h *Handlers) listTasks(w http.ResponseWriter, r *http.Request) {
	teamID, err := requiredQueryInt64(r, "team_id")
	if err != nil {
		writeError(w, err)
		return
	}
	status, err := optionalQueryStatus(r)
	if err != nil {
		writeError(w, err)
		return
	}
	assigneeID, err := optionalQueryInt64(r, "assignee_id")
	if err != nil {
		writeError(w, err)
		return
	}
	limit, offset, err := queryLimitOffset(r)
	if err != nil {
		writeError(w, err)
		return
	}

	tasks, err := h.Tasks.List(r.Context(), userID(r), teamID, status, assigneeID, limit, offset)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, tasks)
}

func (h *Handlers) updateTask(w http.ResponseWriter, r *http.Request) {
	taskID, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	var in service.UpdateTaskInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, domain.ErrBadRequest)
		return
	}

	task, err := h.Tasks.Update(r.Context(), userID(r), taskID, in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, task)
}

func (h *Handlers) taskHistory(w http.ResponseWriter, r *http.Request) {
	taskID, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	history, err := h.Tasks.History(r.Context(), userID(r), taskID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, history)
}

func (h *Handlers) addComment(w http.ResponseWriter, r *http.Request) {
	taskID, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	var in service.AddCommentInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, domain.ErrBadRequest)
		return
	}

	comment, err := h.Tasks.AddComment(r.Context(), userID(r), taskID, in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusCreated, comment)
}

func (h *Handlers) listComments(w http.ResponseWriter, r *http.Request) {
	taskID, err := pathID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	comments, err := h.Tasks.Comments(r.Context(), userID(r), taskID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, http.StatusOK, comments)
}
