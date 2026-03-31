package api

import (
	"net/http"

	"github.com/rana-remote/rana-remote/internal/store"
)

func traceIDFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	return traceIDFromContext(r.Context())
}

func withTraceIDPayload(payload map[string]any, traceID string) map[string]any {
	if payload == nil || traceID == "" {
		return payload
	}
	if _, exists := payload["trace_id"]; !exists {
		payload["trace_id"] = traceID
	}
	return payload
}

func withAuditTrace(r *http.Request, logRec store.AuditLog) store.AuditLog {
	if logRec.TraceID == "" {
		logRec.TraceID = traceIDFromRequest(r)
	}
	return logRec
}
