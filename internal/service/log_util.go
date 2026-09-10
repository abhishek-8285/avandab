package service

import (
	"context"
	"log/slog"

	"transport-app/internal/auth"
)

// logErr warns about a non-fatal store error inside a dashboard partial
// (errgroup branch) without failing the whole page: the dashboard must
// still render with the data branches that succeeded. Carries the request
// ID from the context so the warn line joins its request in the log stream.
func logErr(ctx context.Context, log *slog.Logger, op string, err error) {
	reqID, _ := ctx.Value(auth.ContextReqID).(string)
	log.Warn("dashboard partial failed",
		"op", op,
		"error", err,
		"request_id", reqID)
}
