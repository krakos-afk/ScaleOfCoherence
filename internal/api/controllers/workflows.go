package controllers

import (
	"bytes"
	"encoding/json"
	"net/http"

	"ScaleOfCoherence/internal/api/dto"
	"ScaleOfCoherence/internal/workflows"
)

type WorkflowController struct {
	svc *workflows.Service
}

func NewWorkflowController(svc *workflows.Service) *WorkflowController {
	return &WorkflowController{svc: svc}
}

func (c *WorkflowController) Create(w http.ResponseWriter, r *http.Request) {
	var req dto.WorkflowRequest
	if !decode(w, r, &req) {
		return
	}
	if req.Name == "" {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}
	wf, err := c.svc.Create(r.Context(), req)
	respond(w, http.StatusCreated, wf, err)
}

func (c *WorkflowController) List(w http.ResponseWriter, r *http.Request) {
	limit, offset := paging(r)
	page, err := c.svc.List(r.Context(), limit, offset)
	respond(w, http.StatusOK, page, err)
}

func (c *WorkflowController) Get(w http.ResponseWriter, r *http.Request) {
	wf, err := c.svc.Get(r.Context(), r.PathValue("id"))
	respond(w, http.StatusOK, wf, err)
}

func (c *WorkflowController) Update(w http.ResponseWriter, r *http.Request) {
	var req dto.WorkflowRequest
	if !decode(w, r, &req) {
		return
	}
	if req.Name == "" {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}
	wf, err := c.svc.Update(r.Context(), r.PathValue("id"), req)
	respond(w, http.StatusOK, wf, err)
}

func (c *WorkflowController) Delete(w http.ResponseWriter, r *http.Request) {
	noContent(w, c.svc.Delete(r.Context(), r.PathValue("id")))
}

func (c *WorkflowController) ListStages(w http.ResponseWriter, r *http.Request) {
	s, err := c.svc.ListStages(r.Context(), r.PathValue("id"))
	respond(w, http.StatusOK, s, err)
}

func (c *WorkflowController) GetStage(w http.ResponseWriter, r *http.Request) {
	s, err := c.svc.GetStage(r.Context(), r.PathValue("id"), r.PathValue("stageID"))
	respond(w, http.StatusOK, s, err)
}

func (c *WorkflowController) CreateStage(w http.ResponseWriter, r *http.Request) {
	req, ok := stageRequest(w, r)
	if !ok {
		return
	}
	s, err := c.svc.CreateStage(r.Context(), r.PathValue("id"), req)
	respond(w, http.StatusCreated, s, err)
}

func (c *WorkflowController) UpdateStage(w http.ResponseWriter, r *http.Request) {
	req, ok := stageRequest(w, r)
	if !ok {
		return
	}
	s, err := c.svc.UpdateStage(r.Context(), r.PathValue("id"), r.PathValue("stageID"), req)
	respond(w, http.StatusOK, s, err)
}

func (c *WorkflowController) DeleteStage(w http.ResponseWriter, r *http.Request) {
	noContent(w, c.svc.DeleteStage(r.Context(), r.PathValue("id"), r.PathValue("stageID")))
}

func stageRequest(w http.ResponseWriter, r *http.Request) (dto.StageRequest, bool) {
	var req dto.StageRequest
	if !decode(w, r, &req) {
		return req, false
	}
	if len(req.CompletionRequirements) == 0 {
		req.CompletionRequirements = json.RawMessage("[]")
	}
	hasApprover := req.ApproverUserID != nil && *req.ApproverUserID != ""
	if !hasApprover {
		req.ApproverUserID = nil
	}
	isArray := bytes.HasPrefix(bytes.TrimSpace(req.CompletionRequirements), []byte("["))
	if req.Name == "" || req.AssignedTeamID == "" || req.Position < 0 || !isArray || req.ApprovalRequired != hasApprover {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return req, false
	}
	return req, true
}
