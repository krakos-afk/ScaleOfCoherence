package workflows

import (
	"context"

	"ScaleOfCoherence/internal/api/dto"
	"ScaleOfCoherence/internal/auth"
	"ScaleOfCoherence/internal/auth/identity"
	"ScaleOfCoherence/internal/database/repository"
)

type Service struct {
	repo *repository.WorkflowRepo
}

func NewService(repo *repository.WorkflowRepo) *Service {
	return &Service{repo: repo}
}

func (s *Service) Create(ctx context.Context, req dto.WorkflowRequest) (dto.Workflow, error) {
	id, err := admin(ctx)
	if err != nil {
		return dto.Workflow{}, err
	}
	return s.repo.Create(ctx, id.OrgID, id.UserID, req)
}

func (s *Service) List(ctx context.Context, limit, offset int) (dto.Page[dto.Workflow], error) {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return dto.Page[dto.Workflow]{}, err
	}
	return s.repo.List(ctx, id.OrgID, limit, offset)
}

func (s *Service) Get(ctx context.Context, workflowID string) (dto.Workflow, error) {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return dto.Workflow{}, err
	}
	return s.repo.Get(ctx, id.OrgID, workflowID)
}

func (s *Service) Update(ctx context.Context, workflowID string, req dto.WorkflowRequest) (dto.Workflow, error) {
	id, err := admin(ctx)
	if err != nil {
		return dto.Workflow{}, err
	}
	return s.repo.Update(ctx, id.OrgID, workflowID, req)
}

func (s *Service) Delete(ctx context.Context, workflowID string) error {
	id, err := admin(ctx)
	if err != nil {
		return err
	}
	return s.repo.Delete(ctx, id.OrgID, workflowID)
}

func (s *Service) ListStages(ctx context.Context, workflowID string) ([]dto.Stage, error) {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.repo.Get(ctx, id.OrgID, workflowID); err != nil {
		return nil, err
	}
	return s.repo.ListStages(ctx, id.OrgID, workflowID)
}

func (s *Service) GetStage(ctx context.Context, workflowID, stageID string) (dto.Stage, error) {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return dto.Stage{}, err
	}
	return s.repo.GetStage(ctx, id.OrgID, workflowID, stageID)
}

func (s *Service) CreateStage(ctx context.Context, workflowID string, req dto.StageRequest) (dto.Stage, error) {
	id, err := admin(ctx)
	if err != nil {
		return dto.Stage{}, err
	}
	if _, err := s.repo.Get(ctx, id.OrgID, workflowID); err != nil {
		return dto.Stage{}, err
	}
	return s.repo.CreateStage(ctx, id.OrgID, workflowID, req)
}

func (s *Service) UpdateStage(ctx context.Context, workflowID, stageID string, req dto.StageRequest) (dto.Stage, error) {
	id, err := admin(ctx)
	if err != nil {
		return dto.Stage{}, err
	}
	return s.repo.UpdateStage(ctx, id.OrgID, workflowID, stageID, req)
}

func (s *Service) DeleteStage(ctx context.Context, workflowID, stageID string) error {
	id, err := admin(ctx)
	if err != nil {
		return err
	}
	return s.repo.DeleteStage(ctx, id.OrgID, workflowID, stageID)
}

func admin(ctx context.Context) (identity.Identity, error) {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return id, err
	}
	if !id.IsAdmin() {
		return id, auth.ErrForbidden
	}
	return id, nil
}
