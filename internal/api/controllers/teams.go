package controllers

import (
	"net/http"
	"strconv"

	"ScaleOfCoherence/internal/api/dto"
	"ScaleOfCoherence/internal/teams"
)

type TeamController struct {
	svc *teams.Service
}

func NewTeamController(svc *teams.Service) *TeamController {
	return &TeamController{svc: svc}
}

func (c *TeamController) Create(w http.ResponseWriter, r *http.Request) {
	var req dto.TeamRequest
	if !decode(w, r, &req) {
		return
	}
	if req.Name == "" {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}
	t, err := c.svc.Create(r.Context(), req.Name)
	respond(w, http.StatusCreated, t, err)
}

func (c *TeamController) List(w http.ResponseWriter, r *http.Request) {
	limit, offset := paging(r)
	teams, err := c.svc.List(r.Context(), limit, offset)
	respond(w, http.StatusOK, teams, err)
}

func (c *TeamController) Get(w http.ResponseWriter, r *http.Request) {
	t, err := c.svc.Get(r.Context(), r.PathValue("id"))
	respond(w, http.StatusOK, t, err)
}

func (c *TeamController) Update(w http.ResponseWriter, r *http.Request) {
	var req dto.TeamRequest
	if !decode(w, r, &req) {
		return
	}
	if req.Name == "" {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}
	t, err := c.svc.Update(r.Context(), r.PathValue("id"), req.Name)
	respond(w, http.StatusOK, t, err)
}

func (c *TeamController) Delete(w http.ResponseWriter, r *http.Request) {
	noContent(w, c.svc.Delete(r.Context(), r.PathValue("id")))
}

func (c *TeamController) ListMembers(w http.ResponseWriter, r *http.Request) {
	limit, offset := paging(r)
	m, err := c.svc.ListMembers(r.Context(), r.PathValue("id"), limit, offset)
	respond(w, http.StatusOK, m, err)
}

func (c *TeamController) AddMember(w http.ResponseWriter, r *http.Request) {
	var req dto.AddMemberRequest
	if !decode(w, r, &req) {
		return
	}
	if req.Role == "" {
		req.Role = "viewer"
	}
	if req.UserID == "" || (req.Role != "viewer" && req.Role != "editor") {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}
	err := c.svc.AddMember(r.Context(), r.PathValue("id"), req.UserID, req.Role)
	respond(w, http.StatusCreated, dto.TeamMember{UserID: req.UserID, Role: req.Role}, err)
}

func (c *TeamController) RemoveMember(w http.ResponseWriter, r *http.Request) {
	noContent(w, c.svc.RemoveMember(r.Context(), r.PathValue("id"), r.PathValue("userID")))
}

func paging(r *http.Request) (limit, offset int) {
	limit, _ = strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > 100 {
		limit = 10
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	return limit, (page - 1) * limit
}

func respond(w http.ResponseWriter, status int, v any, err error) {
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, status, v)
}

func noContent(w http.ResponseWriter, err error) {
	if err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
