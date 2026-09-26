// Package v1alpha1 implements the HTTP handlers of the UDLM read API: the
// per-state records the control plane writes and the entity view computed from
// them (docs/udlm-native.md, increment 3). Nothing here writes.
package v1alpha1

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	server "github.com/dcm-project/control-plane/internal/udlm/api/server"
	"github.com/dcm-project/control-plane/internal/udlm/records"
)

// Handler serves the UDLM read API over the records store.
type Handler struct {
	store records.Store
}

// NewHandler returns a Handler over the given store.
func NewHandler(store records.Store) *Handler {
	return &Handler{store: store}
}

var _ server.StrictServerInterface = (*Handler)(nil)

const internalErrorDetail = "an internal error occurred"

func newError(errType, title, detail string, status int) server.Error {
	return server.Error{Type: errType, Title: title, Detail: &detail, Status: &status}
}

// ListUdlmEntities pages through the entities that have records.
func (h *Handler) ListUdlmEntities(ctx context.Context, req server.ListUdlmEntitiesRequestObject) (server.ListUdlmEntitiesResponseObject, error) {
	pageSize := 0
	if req.Params.MaxPageSize != nil {
		pageSize = *req.Params.MaxPageSize
	}
	pageToken := ""
	if req.Params.PageToken != nil {
		pageToken = *req.Params.PageToken
	}
	summaries, next, err := h.store.ListEntities(ctx, pageSize, pageToken)
	if err != nil {
		slog.ErrorContext(ctx, "ListUdlmEntities failed", "error", err)
		return server.ListUdlmEntitiesdefaultApplicationProblemPlusJSONResponse{
			Body:       newError("list-error", "Failed to list entities", internalErrorDetail, http.StatusInternalServerError),
			StatusCode: http.StatusInternalServerError,
		}, nil
	}
	out := make([]server.EntitySummary, 0, len(summaries))
	for _, s := range summaries {
		item := server.EntitySummary{
			EntityUuid:     s.EntityUUID,
			TenantUuid:     s.TenantUUID,
			ResourceType:   s.ResourceType,
			TypeVersion:    s.TypeVersion,
			LifecycleState: server.EntitySummaryLifecycleState(s.Lifecycle),
			RecordCount:    s.RecordCount,
		}
		if s.Latest != nil {
			if at, ok := s.Latest.Body["at"].(string); ok {
				item.LatestAt = &at
			}
		}
		out = append(out, item)
	}
	resp := server.EntitySummaryList{Entities: out}
	if next != "" {
		resp.NextPageToken = &next
	}
	return server.ListUdlmEntities200JSONResponse(resp), nil
}

// GetUdlmEntity computes and returns the entity view.
func (h *Handler) GetUdlmEntity(ctx context.Context, req server.GetUdlmEntityRequestObject) (server.GetUdlmEntityResponseObject, error) {
	recs, err := h.store.ListByEntity(ctx, req.EntityUuid)
	if err != nil {
		slog.ErrorContext(ctx, "GetUdlmEntity failed", "entity_uuid", req.EntityUuid, "error", err)
		return server.GetUdlmEntitydefaultApplicationProblemPlusJSONResponse{
			Body:       newError("get-error", "Failed to read entity", internalErrorDetail, http.StatusInternalServerError),
			StatusCode: http.StatusInternalServerError,
		}, nil
	}
	bodies := make([]map[string]any, 0, len(recs))
	for i := range recs {
		bodies = append(bodies, recs[i].Body)
	}
	view := records.Fold(bodies)
	if view == nil {
		return server.GetUdlmEntity404ApplicationProblemPlusJSONResponse{
			NotFoundApplicationProblemPlusJSONResponse: server.NotFoundApplicationProblemPlusJSONResponse(
				newError("not-found", "No records for this entity", "entity "+req.EntityUuid+" has no UDLM records", http.StatusNotFound)),
		}, nil
	}
	return entityViewResponse{view: toEntityView(view)}, nil
}

// entityViewResponse writes the view through EntityView's own MarshalJSON. The
// generated GetUdlmEntity200JSONResponse is a distinct named type, so it does
// not carry that method and would drop the additional properties (every view
// key beyond uuid / entity_uuid / lifecycle_state / states).
type entityViewResponse struct {
	view server.EntityView
}

func (r entityViewResponse) VisitGetUdlmEntityResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(r.view)
}

// ListUdlmEntityRecords returns the entity's records, oldest first.
func (h *Handler) ListUdlmEntityRecords(ctx context.Context, req server.ListUdlmEntityRecordsRequestObject) (server.ListUdlmEntityRecordsResponseObject, error) {
	recs, err := h.store.ListByEntity(ctx, req.EntityUuid)
	if err != nil && !errors.Is(err, records.ErrNotFound) {
		slog.ErrorContext(ctx, "ListUdlmEntityRecords failed", "entity_uuid", req.EntityUuid, "error", err)
		return server.ListUdlmEntityRecordsdefaultApplicationProblemPlusJSONResponse{
			Body:       newError("list-error", "Failed to read records", internalErrorDetail, http.StatusInternalServerError),
			StatusCode: http.StatusInternalServerError,
		}, nil
	}
	if len(recs) == 0 {
		return server.ListUdlmEntityRecords404ApplicationProblemPlusJSONResponse{
			NotFoundApplicationProblemPlusJSONResponse: server.NotFoundApplicationProblemPlusJSONResponse(
				newError("not-found", "No records for this entity", "entity "+req.EntityUuid+" has no UDLM records", http.StatusNotFound)),
		}, nil
	}
	out := make([]server.StateRecord, 0, len(recs))
	for i := range recs {
		out = append(out, toStateRecord(recs[i].Body))
	}
	return server.ListUdlmEntityRecords200JSONResponse(server.StateRecordList{Records: out}), nil
}

// toEntityView splits the computed view into the generated type's named fields
// and its additional properties, so it serializes as the registry's shape.
func toEntityView(view map[string]any) server.EntityView {
	out := server.EntityView{
		Uuid:                 stringOf(view["uuid"]),
		EntityUuid:           stringOf(view["entity_uuid"]),
		LifecycleState:       server.EntityViewLifecycleState(stringOf(view["lifecycle_state"])),
		AdditionalProperties: map[string]any{},
	}
	if states, ok := view["states"].(map[string]any); ok {
		out.States = states
	}
	for k, v := range view {
		switch k {
		case "uuid", "entity_uuid", "lifecycle_state", "states":
		default:
			out.AdditionalProperties[k] = v
		}
	}
	return out
}

func toStateRecord(body map[string]any) server.StateRecord {
	out := server.StateRecord{
		RecordType:           server.StateRecordRecordType(stringOf(body["record_type"])),
		State:                server.StateRecordState(stringOf(body["state"])),
		EntityUuid:           stringOf(body["entity_uuid"]),
		RecordUuid:           stringOf(body["record_uuid"]),
		AdditionalProperties: map[string]any{},
	}
	for k, v := range body {
		switch k {
		case "record_type", "state", "entity_uuid", "record_uuid":
		default:
			out.AdditionalProperties[k] = v
		}
	}
	return out
}

func stringOf(v any) string {
	s, _ := v.(string)
	return s
}
