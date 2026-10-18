package teams

import (
	"context"

	"ScaleOfCoherence/internal/api/dto"
	"ScaleOfCoherence/internal/auth"
	"ScaleOfCoherence/internal/auth/identity"
	"ScaleOfCoherence/internal/database/repository"
)

type Service struct {
	repo *repository.TeamRepo
}

func NewService(repo *repository.TeamRepo) *Service {
	return &Service{repo: repo}
}

func (s *Service) Create(ctx context.Context, name string) (dto.Team, error) {
	id, err := admin(ctx)
	if err != nil {
		return dto.Team{}, err
	}
	return s.repo.Create(ctx, id.OrgID, name)
}

func (s *Service) List(ctx context.Context, limit, offset int) (dto.Page[dto.Team], error) {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return dto.Page[dto.Team]{}, err
	}
	return s.repo.List(ctx, id.OrgID, limit, offset)
}

func (s *Service) Get(ctx context.Context, teamID string) (dto.Team, error) {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return dto.Team{}, err
	}
	return s.repo.Get(ctx, id.OrgID, teamID)
}

func (s *Service) Update(ctx context.Context, teamID, name string) (dto.Team, error) {
	id, err := admin(ctx)
	if err != nil {
		return dto.Team{}, err
	}
	return s.repo.Update(ctx, id.OrgID, teamID, name)
}

func (s *Service) Delete(ctx context.Context, teamID string) error {
	id, err := admin(ctx)
	if err != nil {
		return err
	}
	return s.repo.Delete(ctx, id.OrgID, teamID)
}

func (s *Service) ListMembers(ctx context.Context, teamID string, limit, offset int) (dto.Page[dto.TeamMember], error) {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return dto.Page[dto.TeamMember]{}, err
	}
	if _, err := s.repo.Get(ctx, id.OrgID, teamID); err != nil {
		return dto.Page[dto.TeamMember]{}, err
	}
	return s.repo.ListMembers(ctx, id.OrgID, teamID, limit, offset)
}

func (s *Service) AddMember(ctx context.Context, teamID, userID, role string) error {
	id, err := admin(ctx)
	if err != nil {
		return err
	}
	return s.repo.AddMember(ctx, id.OrgID, teamID, userID, role)
}

func (s *Service) RemoveMember(ctx context.Context, teamID, userID string) error {
	id, err := admin(ctx)
	if err != nil {
		return err
	}
	return s.repo.RemoveMember(ctx, id.OrgID, teamID, userID)
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
