package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/dcm-project/control-plane/internal/gitops/store/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrGitRepositoryNotFound = errors.New("git repository not found")
	ErrGitRepositoryIDTaken  = errors.New("git repository ID already taken")
	ErrDisplayNameTaken      = errors.New("display_name already taken")
)

// GitRepositoryListOptions contains options for listing git repositories.
type GitRepositoryListOptions struct {
	PageToken *string
	PageSize  int
}

// GitRepositoryListResult contains the result of a List operation.
type GitRepositoryListResult struct {
	GitRepositories model.GitRepositoryList
	NextPageToken   string
}

type GitRepository interface {
	List(ctx context.Context, opts *GitRepositoryListOptions) (*GitRepositoryListResult, error)
	ListAll(ctx context.Context) (model.GitRepositoryList, error)
	Get(ctx context.Context, id string) (*model.GitRepository, error)
	Create(ctx context.Context, repo model.GitRepository) (*model.GitRepository, error)
	Update(ctx context.Context, repo model.GitRepository) (*model.GitRepository, error)
	Delete(ctx context.Context, id string) error
	UpdateSyncStatus(ctx context.Context, id, syncState, statusMessage, lastSyncedCommit string) error
}

type GitRepositoryStore struct {
	db *gorm.DB
}

var _ GitRepository = (*GitRepositoryStore)(nil)

func NewGitRepository(db *gorm.DB) GitRepository {
	return &GitRepositoryStore{db: db}
}

func (s *GitRepositoryStore) List(ctx context.Context, opts *GitRepositoryListOptions) (*GitRepositoryListResult, error) {
	var repos model.GitRepositoryList
	query := s.db.WithContext(ctx)

	pageSize := 100
	if opts != nil && opts.PageSize > 0 {
		pageSize = opts.PageSize
	}

	offset := 0
	if opts != nil && opts.PageToken != nil && *opts.PageToken != "" {
		var err error
		offset, err = decodePageToken(*opts.PageToken)
		if err != nil {
			return nil, err
		}
	}

	query = query.Order("id ASC").Limit(pageSize + 1).Offset(offset)

	if err := query.Find(&repos).Error; err != nil {
		return nil, err
	}

	result := &GitRepositoryListResult{
		GitRepositories: repos,
	}

	if len(repos) > pageSize {
		result.GitRepositories = repos[:pageSize]
		nextOffset := offset + pageSize
		nextPageToken, err := encodePageToken(nextOffset)
		if err != nil {
			return nil, err
		}
		result.NextPageToken = nextPageToken
	}

	return result, nil
}

func (s *GitRepositoryStore) ListAll(ctx context.Context) (model.GitRepositoryList, error) {
	var repos model.GitRepositoryList
	if err := s.db.WithContext(ctx).Order("id ASC").Find(&repos).Error; err != nil {
		return nil, err
	}
	if repos == nil {
		repos = model.GitRepositoryList{}
	}
	return repos, nil
}

func (s *GitRepositoryStore) Get(ctx context.Context, id string) (*model.GitRepository, error) {
	var repo model.GitRepository
	if err := s.db.WithContext(ctx).First(&repo, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrGitRepositoryNotFound
		}
		return nil, err
	}
	return &repo, nil
}

func (s *GitRepositoryStore) Create(ctx context.Context, repo model.GitRepository) (*model.GitRepository, error) {
	if err := s.db.WithContext(ctx).Clauses(clause.Returning{}).Select("*").Create(&repo).Error; err != nil {
		return nil, s.mapUniqueConstraintError(ctx, err, repo)
	}
	return &repo, nil
}

func (s *GitRepositoryStore) Update(ctx context.Context, repo model.GitRepository) (*model.GitRepository, error) {
	result := s.db.WithContext(ctx).Model(&repo).
		Select("display_name", "url", "branch", "path", "interval_seconds").
		Clauses(clause.Returning{}).
		Updates(&repo)
	if result.Error != nil {
		return nil, s.mapUniqueConstraintError(ctx, result.Error, repo)
	}
	if result.RowsAffected == 0 {
		return nil, ErrGitRepositoryNotFound
	}
	return &repo, nil
}

func (s *GitRepositoryStore) Delete(ctx context.Context, id string) error {
	result := s.db.WithContext(ctx).Where("id = ?", id).Delete(&model.GitRepository{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrGitRepositoryNotFound
	}
	return nil
}

func (s *GitRepositoryStore) UpdateSyncStatus(ctx context.Context, id, syncState, statusMessage, lastSyncedCommit string) error {
	now := time.Now().UTC()
	updates := map[string]any{
		"sync_state":     syncState,
		"status_message": statusMessage,
		"last_sync_time": &now,
	}
	if lastSyncedCommit != "" {
		updates["last_synced_commit"] = lastSyncedCommit
	}
	result := s.db.WithContext(ctx).Model(&model.GitRepository{}).
		Where("id = ?", id).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrGitRepositoryNotFound
	}
	return nil
}

func (s *GitRepositoryStore) mapUniqueConstraintError(ctx context.Context, err error, attempted model.GitRepository) error {
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrDuplicatedKey) {
		if !strings.Contains(strings.ToLower(err.Error()), "unique") &&
			!strings.Contains(err.Error(), "duplicate key") {
			return err
		}
	}

	checks := []struct {
		sentinel error
		query    *gorm.DB
	}{
		{ErrGitRepositoryIDTaken, s.db.WithContext(ctx).Where("id = ?", attempted.ID).Limit(1)},
		{ErrDisplayNameTaken, s.db.WithContext(ctx).Where("display_name = ?", attempted.DisplayName).Limit(1)},
	}

	for _, c := range checks {
		var row model.GitRepository
		dberr := c.query.First(&row).Error
		if dberr == nil {
			return c.sentinel
		}
		if !errors.Is(dberr, gorm.ErrRecordNotFound) {
			return err
		}
	}

	return err
}
