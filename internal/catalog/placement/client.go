// Package placement provides a client for the Placement Manager service.
package placement

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dcm-project/control-plane/internal/placement/types"
)

// PlacementError represents a structured error from the Placement Manager,
// preserving the HTTP status code for upstream handling.
type PlacementError struct {
	StatusCode int
	Body       string
}

func (e *PlacementError) Error() string {
	return fmt.Sprintf("placement manager returned status %d: %s", e.StatusCode, e.Body)
}

// CreateRunRequest is the request body for creating a placement run.
type CreateRunRequest = types.CreateRunRequest

// ResourceInput is one node in a CreateRun request graph.
type ResourceInput = types.ResourceInput

// Run is the response from CreateRun.
type Run struct {
	RunID                 string     `json:"run_id"`
	Path                  string     `json:"path,omitempty"`
	CatalogItemInstanceID string     `json:"catalog_item_instance_id"`
	Resources             []Resource `json:"resources"`
}

// Resource is a slim resource view for placement.
type Resource struct {
	ID                string         `json:"id,omitempty"`
	Name              string         `json:"name,omitempty"`
	Path              string         `json:"path,omitempty"`
	Spec              map[string]any `json:"spec,omitempty"`
	RequiresResources []string       `json:"requires_resources,omitempty"`
}

// Client defines the interface for interacting with Placement.
type Client interface {
	CreateRun(ctx context.Context, req CreateRunRequest) (*Run, error)
	DeleteRun(ctx context.Context, runID string) error
	RehydrateResource(ctx context.Context, runID string, newRunID string) (*Resource, error)
}

type client struct {
	baseURL    string
	httpClient *http.Client
	logger     *slog.Logger
}

type rehydrateRequestBody struct {
	NewRunID string `json:"new_run_id"`
}

// NewClient creates a remote placement client for subsystem tests and split
// deployments. Production monolith wiring uses NewLocalClient instead.
func NewClient(baseURL string, timeout time.Duration, logger *slog.Logger) (Client, error) {
	apiBase, err := apiBaseURL(baseURL)
	if err != nil {
		return nil, err
	}

	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	logger.Info("Placement manager client created", "url", apiBase, "timeout", timeout)
	return &client{
		baseURL:    apiBase,
		httpClient: &http.Client{Timeout: timeout},
		logger:     logger.With("component", "placement"),
	}, nil
}

func apiBaseURL(baseURL string) (string, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return "", fmt.Errorf("placement manager URL is required")
	}
	return url.JoinPath(strings.TrimRight(baseURL, "/"), "api", "v1alpha1")
}

// CreateRun creates a placement run in the Placement Manager.
func (c *client) CreateRun(ctx context.Context, req CreateRunRequest) (*Run, error) {
	runId := req.RunId
	c.logger.InfoContext(ctx, "Creating run in placement manager",
		"catalog_item_instance_id", req.CatalogItemInstanceId,
		"run_id", runId,
	)

	endpoint := c.baseURL + "/runs"
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal placement create request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create placement request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		c.logger.ErrorContext(ctx, "Placement manager create run call failed", "run_id", runId, "error", err)
		return nil, fmt.Errorf("failed to call placement manager: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read placement create run response: %w", err)
	}

	if resp.StatusCode != http.StatusAccepted {
		c.logger.ErrorContext(ctx, "Placement manager returned unexpected status",
			"status", resp.StatusCode,
		)
		return nil, &PlacementError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}

	var parsed Run
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("failed to decode placement create run response: %w", err)
	}

	c.logger.InfoContext(ctx, "Run created in placement manager", "run_id", parsed.RunID)
	return &parsed, nil
}

// DeleteRun deletes a placement run by run_id.
func (c *client) DeleteRun(ctx context.Context, runID string) error {
	c.logger.InfoContext(ctx, "Deleting run from placement manager", "run_id", runID)

	endpoint, err := url.JoinPath(c.baseURL, "runs", runID)
	if err != nil {
		return fmt.Errorf("failed to build placement delete URL: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to create placement delete request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		c.logger.ErrorContext(ctx, "Placement manager delete run call failed",
			"run_id", runID,
			"error", err,
		)
		return fmt.Errorf("failed to call placement manager: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read placement delete response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		c.logger.ErrorContext(ctx, "Placement manager delete returned unexpected status",
			"run_id", runID,
			"status", resp.StatusCode,
		)
		return fmt.Errorf("placement manager returned status %d: %s", resp.StatusCode, string(respBody))
	}

	c.logger.InfoContext(ctx, "Run deleted from placement manager", "run_id", runID)
	return nil
}

// RehydrateResource rehydrates a placement run in the Placement Manager.
func (c *client) RehydrateResource(ctx context.Context, runID string, newRunID string) (*Resource, error) {
	c.logger.InfoContext(ctx, "Rehydrating run in placement manager",
		"run_id", runID,
		"new_run_id", newRunID,
	)

	endpoint := c.baseURL + "/runs/" + url.PathEscape(runID) + ":rehydrate"
	payload, err := json.Marshal(rehydrateRequestBody{NewRunID: newRunID})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal placement rehydrate request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create placement rehydrate request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		c.logger.ErrorContext(ctx, "Placement manager rehydrate run call failed",
			"run_id", runID,
			"error", err,
		)
		return nil, fmt.Errorf("failed to call placement manager: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read placement rehydrate response: %w", err)
	}

	if resp.StatusCode != http.StatusAccepted {
		c.logger.ErrorContext(ctx, "Placement manager returned unexpected status",
			"run_id", runID,
			"status", resp.StatusCode,
		)
		return nil, &PlacementError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}

	var parsed Resource
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("failed to decode placement rehydrate response: %w", err)
	}

	c.logger.InfoContext(ctx, "Run rehydrated in placement manager",
		"run_id", runID,
		"new_run_id", newRunID,
	)
	return &parsed, nil
}
