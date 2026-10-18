package dto

import (
	"encoding/json"
	"time"
)

type WorkflowRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type Workflow struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

type StageRequest struct {
	Name                   string          `json:"name"`
	Description            string          `json:"description"`
	AssignedTeamID         string          `json:"assigned_team_id"`
	Position               int             `json:"position"`
	CompletionRequirements json.RawMessage `json:"completion_requirements"`
	ApprovalRequired       bool            `json:"approval_required"`
	ApproverUserID         *string         `json:"approver_user_id"`
}

type Stage struct {
	ID                     string          `json:"id"`
	WorkflowID             string          `json:"workflow_id"`
	Name                   string          `json:"name"`
	Description            string          `json:"description"`
	AssignedTeamID         string          `json:"assigned_team_id"`
	Position               int             `json:"position"`
	CompletionRequirements json.RawMessage `json:"completion_requirements"`
	ApprovalRequired       bool            `json:"approval_required"`
	ApproverUserID         *string         `json:"approver_user_id"`
	CreatedAt              time.Time       `json:"created_at"`
	UpdatedAt              time.Time       `json:"updated_at"`
}
