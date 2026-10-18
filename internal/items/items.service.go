package items

import (
	"context"

	"ScaleOfCoherence/internal/api/dto"
	"ScaleOfCoherence/internal/auth"
	"ScaleOfCoherence/internal/auth/identity"
	"ScaleOfCoherence/internal/database/repository"
)

type Service struct {
	repo *repository.ItemRepo
}

func NewService(repo *repository.ItemRepo) *Service {
	return &Service{repo: repo}
}

func (s *Service) Create(ctx context.Context, req dto.ItemRequest) (dto.Item, error) {
	id, err := admin(ctx)
	if err != nil {
		return dto.Item{}, err
	}
	return s.repo.Create(ctx, id.OrgID, id.UserID, req)
}

func (s *Service) List(ctx context.Context, limit, offset int) (dto.Page[dto.Item], error) {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return dto.Page[dto.Item]{}, err
	}
	return s.repo.List(ctx, id.OrgID, limit, offset)
}

func (s *Service) Get(ctx context.Context, itemID string) (dto.Item, error) {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return dto.Item{}, err
	}
	return s.repo.Get(ctx, id.OrgID, itemID)
}

func (s *Service) Update(ctx context.Context, itemID string, req dto.ItemRequest) (dto.Item, error) {
	id, err := admin(ctx)
	if err != nil {
		return dto.Item{}, err
	}
	return s.repo.Update(ctx, id.OrgID, itemID, req)
}

func (s *Service) Delete(ctx context.Context, itemID string) error {
	id, err := admin(ctx)
	if err != nil {
		return err
	}
	return s.repo.Delete(ctx, id.OrgID, itemID)
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
