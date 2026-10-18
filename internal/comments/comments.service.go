package comments

import (
	"context"

	"ScaleOfCoherence/internal/api/dto"
	"ScaleOfCoherence/internal/auth/identity"
	"ScaleOfCoherence/internal/database/repository"
)

type Service struct {
	repo *repository.CommentRepo
}

func NewService(repo *repository.CommentRepo) *Service {
	return &Service{repo: repo}
}

// List returns a page of an item's comments. Any org member may read them.
func (s *Service) List(ctx context.Context, itemID string, limit, offset int) (dto.Page[dto.Comment], error) {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return dto.Page[dto.Comment]{}, err
	}
	return s.repo.List(ctx, id.OrgID, itemID, limit, offset)
}

// Create adds a comment anchored to the item's current version.
// Team-role checks live in the repository.
func (s *Service) Create(ctx context.Context, itemID, body string) (dto.Comment, error) {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return dto.Comment{}, err
	}
	return s.repo.Create(ctx, id.OrgID, itemID, id.UserID, body, id.IsAdmin())
}

// Delete removes a comment if the caller wrote it, is an org admin, or leads
// the team responsible for the item's current stage.
func (s *Service) Delete(ctx context.Context, itemID string, commentID int64) error {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return err
	}
	return s.repo.Delete(ctx, id.OrgID, itemID, commentID, id.UserID, id.IsAdmin())
}
