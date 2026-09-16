// Package service implements the GitRepository business logic.
package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	v1alpha1 "github.com/dcm-project/control-plane/api/gitops/v1alpha1"
	"github.com/dcm-project/control-plane/internal/gitops/store"
	"github.com/dcm-project/control-plane/internal/gitops/store/model"
	"github.com/google/uuid"
)

const (
	minIntervalSeconds = 10
	maxIntervalSeconds = 86400
)

var (
	ErrNotFound        = errors.New("git repository not found")
	ErrAlreadyExists   = errors.New("git repository already exists")
	ErrInvalidArgument = errors.New("invalid argument")
)

type GitRepositoryService interface {
	List(ctx context.Context, pageToken *string, maxPageSize *int32) (*v1alpha1.GitRepositoryList, error)
	Get(ctx context.Context, id string) (*v1alpha1.GitRepository, error)
	Create(ctx context.Context, req v1alpha1.GitRepository, clientID *string) (*v1alpha1.GitRepository, error)
	Update(ctx context.Context, id string, req v1alpha1.GitRepository) (*v1alpha1.GitRepository, error)
	Delete(ctx context.Context, id string) error
}

type gitRepositoryService struct {
	store store.Store
}

func NewGitRepositoryService(store store.Store) GitRepositoryService {
	return &gitRepositoryService{store: store}
}

func (s *gitRepositoryService) List(ctx context.Context, pageToken *string, maxPageSize *int32) (*v1alpha1.GitRepositoryList, error) {
	opts := &store.GitRepositoryListOptions{
		PageToken: pageToken,
	}
	if maxPageSize != nil {
		opts.PageSize = int(*maxPageSize)
	}

	result, err := s.store.GitRepository().List(ctx, opts)
	if err != nil {
		return nil, err
	}

	apiTypes := make([]v1alpha1.GitRepository, len(result.GitRepositories))
	for i, m := range result.GitRepositories {
		apiTypes[i] = modelToAPI(&m)
	}

	return &v1alpha1.GitRepositoryList{
		Results:       apiTypes,
		NextPageToken: result.NextPageToken,
	}, nil
}

func (s *gitRepositoryService) Get(ctx context.Context, id string) (*v1alpha1.GitRepository, error) {
	repo, err := s.store.GitRepository().Get(ctx, id)
	if err != nil {
		return nil, mapStoreError(err)
	}
	result := modelToAPI(repo)
	return &result, nil
}

func (s *gitRepositoryService) Create(ctx context.Context, req v1alpha1.GitRepository, clientID *string) (*v1alpha1.GitRepository, error) {
	id := getOrGenerateID(clientID)

	if err := validateGitRepository(req); err != nil {
		return nil, err
	}

	m := apiToModel(id, req)
	created, err := s.store.GitRepository().Create(ctx, m)
	if err != nil {
		return nil, mapStoreError(err)
	}
	result := modelToAPI(created)
	return &result, nil
}

func (s *gitRepositoryService) Update(ctx context.Context, id string, req v1alpha1.GitRepository) (*v1alpha1.GitRepository, error) {
	if err := validateGitRepository(req); err != nil {
		return nil, err
	}

	m := apiToModel(id, req)
	updated, err := s.store.GitRepository().Update(ctx, m)
	if err != nil {
		return nil, mapStoreError(err)
	}
	result := modelToAPI(updated)
	return &result, nil
}

func (s *gitRepositoryService) Delete(ctx context.Context, id string) error {
	err := s.store.GitRepository().Delete(ctx, id)
	if err != nil {
		return mapStoreError(err)
	}
	return nil
}

func validateGitRepository(req v1alpha1.GitRepository) error {
	if req.DisplayName == "" {
		return fmt.Errorf("%w: display_name is required", ErrInvalidArgument)
	}
	if len(req.DisplayName) > 63 {
		return fmt.Errorf("%w: display_name must be at most 63 characters", ErrInvalidArgument)
	}
	if req.Spec.Url == "" {
		return fmt.Errorf("%w: spec.url is required", ErrInvalidArgument)
	}
	u, err := url.Parse(req.Spec.Url)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("%w: spec.url must be a valid URL with scheme and host", ErrInvalidArgument)
	}
	switch u.Scheme {
	case "https", "http", "ssh":
	default:
		return fmt.Errorf("%w: spec.url scheme must be https, http, or ssh", ErrInvalidArgument)
	}
	if u.Port() != "" {
		return fmt.Errorf("%w: spec.url must not include a port", ErrInvalidArgument)
	}
	host := u.Hostname()
	if host == "localhost" || strings.HasPrefix(host, "127.") || host == "::1" || host == "0.0.0.0" || host == "metadata.google.internal" || host == "169.254.169.254" {
		return fmt.Errorf("%w: spec.url must not reference localhost or link-local addresses", ErrInvalidArgument)
	}
	if req.Spec.IntervalSeconds != nil {
		v := int(*req.Spec.IntervalSeconds)
		if v < minIntervalSeconds || v > maxIntervalSeconds {
			return fmt.Errorf("%w: spec.interval_seconds must be between %d and %d", ErrInvalidArgument, minIntervalSeconds, maxIntervalSeconds)
		}
	}
	if req.Spec.Path != nil {
		p := filepath.Clean(*req.Spec.Path)
		if filepath.IsAbs(p) {
			return fmt.Errorf("%w: spec.path must be a relative path", ErrInvalidArgument)
		}
		if p == ".." || strings.HasPrefix(p, ".."+string(filepath.Separator)) {
			return fmt.Errorf("%w: spec.path must not escape the repository root", ErrInvalidArgument)
		}
	}
	return nil
}

func getOrGenerateID(clientID *string) string {
	if clientID != nil && *clientID != "" {
		return *clientID
	}
	return uuid.New().String()
}

func modelToAPI(m *model.GitRepository) v1alpha1.GitRepository {
	path := fmt.Sprintf("git-repositories/%s", m.ID)
	status := modelToStatus(m)

	repo := v1alpha1.GitRepository{
		Uid:         &m.ID,
		ApiVersion:  m.ApiVersion,
		DisplayName: m.DisplayName,
		Spec: v1alpha1.GitRepositorySpec{
			Url: m.URL,
		},
		Status:     &status,
		Path:       &path,
		CreateTime: &m.CreateTime,
		UpdateTime: &m.UpdateTime,
	}

	branch := m.Branch
	repo.Spec.Ref = &struct {
		Branch *string `json:"branch,omitempty"`
	}{Branch: &branch}

	specPath := m.Path
	repo.Spec.Path = &specPath

	interval := int32(m.IntervalSeconds)
	repo.Spec.IntervalSeconds = &interval

	return repo
}

func modelToStatus(m *model.GitRepository) v1alpha1.GitRepositoryStatus {
	syncState := v1alpha1.GitRepositoryStatusSyncState(m.SyncState)
	status := v1alpha1.GitRepositoryStatus{
		SyncState:    &syncState,
		LastSyncTime: m.LastSyncTime,
	}
	if m.LastSyncedCommit != "" {
		status.LastSyncedCommit = &m.LastSyncedCommit
	}
	if m.StatusMessage != "" {
		status.Message = &m.StatusMessage
	}
	return status
}

func apiToModel(id string, req v1alpha1.GitRepository) model.GitRepository {
	m := model.GitRepository{
		ID:              id,
		ApiVersion:      req.ApiVersion,
		DisplayName:     req.DisplayName,
		URL:             req.Spec.Url,
		Branch:          "main",
		Path:            ".",
		IntervalSeconds: 60,
		SyncState:       "PENDING",
	}

	if req.Spec.Ref != nil && req.Spec.Ref.Branch != nil {
		m.Branch = *req.Spec.Ref.Branch
	}
	if req.Spec.Path != nil {
		m.Path = *req.Spec.Path
	}
	if req.Spec.IntervalSeconds != nil {
		m.IntervalSeconds = int(*req.Spec.IntervalSeconds)
	}
	return m
}

func mapStoreError(err error) error {
	switch {
	case errors.Is(err, store.ErrGitRepositoryNotFound):
		return fmt.Errorf("%w: %s", ErrNotFound, err.Error())
	case errors.Is(err, store.ErrGitRepositoryIDTaken):
		return fmt.Errorf("%w: ID already taken", ErrAlreadyExists)
	case errors.Is(err, store.ErrDisplayNameTaken):
		return fmt.Errorf("%w: display_name already taken", ErrAlreadyExists)
	case errors.Is(err, store.ErrInvalidPageToken):
		return fmt.Errorf("%w: %s", ErrInvalidArgument, err.Error())
	default:
		return err
	}
}
