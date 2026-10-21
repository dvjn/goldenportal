package admin

import (
	"math"
	"strconv"
	"time"

	"github.com/dvjn/goldport/internal/audit"
	"gofr.dev/pkg/gofr"
)

const (
	defaultAuditLimit = 50
	maxAuditLimit     = 200
)

type auditActor struct {
	UserID string `json:"user_id,omitempty"`
	Label  string `json:"label"`
}

type auditTarget struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

type auditEntry struct {
	ID      int64          `json:"id"`
	At      time.Time      `json:"at"`
	Actor   auditActor     `json:"actor"`
	Action  audit.Action   `json:"action"`
	Target  auditTarget    `json:"target"`
	Details map[string]any `json:"details"`
}

type auditResponse struct {
	Entries []auditEntry `json:"entries"`
	Offset  int          `json:"offset"`
	Limit   int          `json:"limit"`
	Total   int          `json:"total"`
}

func (s *Service) listAudit(ctx *gofr.Context) (any, error) {
	page := audit.Page{
		Offset: clamp(ctx.Param("offset"), 0, 0, math.MaxInt),
		Limit:  clamp(ctx.Param("limit"), defaultAuditLimit, 1, maxAuditLimit),
	}
	entries, total, err := s.audit.List(ctx, page)
	if err != nil {
		return nil, err
	}

	response := auditResponse{Entries: make([]auditEntry, 0, len(entries)), Offset: page.Offset, Limit: page.Limit, Total: total}
	for _, entry := range entries {
		response.Entries = append(response.Entries, auditEntry{
			ID:      entry.ID,
			At:      entry.At,
			Actor:   auditActor{UserID: entry.Actor.UserID, Label: entry.Actor.Label},
			Action:  entry.Action,
			Target:  auditTarget{Kind: entry.Target.Kind, ID: entry.Target.ID, Name: entry.Target.Name},
			Details: entry.Details,
		})
	}

	return response, nil
}

// clamp parses a query value, falls back when it is not a number and keeps
// it within the bounds.
func clamp(value string, fallback, lowest, highest int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		parsed = fallback
	}

	return min(max(parsed, lowest), highest)
}
