package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rana-remote/rana-remote/internal/store"
)

func (s *Server) appendExecutionLog(ctx context.Context, executionID, level, line string) {
	logRec := store.ExecutionLog{
		ExecutionID: executionID,
		Timestamp:   time.Now().UTC(),
		Level:       level,
		Line:        line,
	}
	_ = s.repo.AppendExecutionLog(ctx, logRec)
	s.publishExecutionLog(logRec)
}

func (s *Server) publishExecutionLog(logRec store.ExecutionLog) {
	s.mu.Lock()
	defer s.mu.Unlock()
	subs := s.streams[logRec.ExecutionID]
	for ch := range subs {
		select {
		case ch <- logRec:
		default:
			// Avoid blocking runner on slow stream consumers.
		}
	}
}

func (s *Server) subscribeExecutionLog(executionID string) (<-chan store.ExecutionLog, func()) {
	ch := make(chan store.ExecutionLog, 128)
	s.mu.Lock()
	if _, ok := s.streams[executionID]; !ok {
		s.streams[executionID] = make(map[chan store.ExecutionLog]struct{})
	}
	s.streams[executionID][ch] = struct{}{}
	s.mu.Unlock()

	cleanup := func() {
		s.mu.Lock()
		if subs, ok := s.streams[executionID]; ok {
			delete(subs, ch)
			if len(subs) == 0 {
				delete(s.streams, executionID)
			}
		}
		s.mu.Unlock()
		close(ch)
	}
	return ch, cleanup
}

func (s *Server) handleExecutionLogs(w http.ResponseWriter, r *http.Request) {
	executionID := r.PathValue("id")
	if _, err := s.repo.GetExecution(r.Context(), executionID); err != nil {
		s.writeError(w, r, http.StatusNotFound, "execution.not_found", nil)
		return
	}
	logs, err := s.repo.ListExecutionLogs(r.Context(), executionID)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal.error", nil)
		return
	}

	levelFilter := strings.TrimSpace(r.URL.Query().Get("level"))
	contains := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("contains")))
	filtered := make([]store.ExecutionLog, 0, len(logs))
	for _, item := range logs {
		if levelFilter != "" && !strings.EqualFold(item.Level, levelFilter) {
			continue
		}
		if contains != "" && !strings.Contains(strings.ToLower(item.Line), contains) {
			continue
		}
		filtered = append(filtered, item)
	}

	page := parsePageSpec(r)
	paged, meta := paginate(filtered, page)
	if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("download")), "1") {
		filename := executionID + ".log"
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
		for _, item := range filtered {
			_, _ = fmt.Fprintf(w, "[%s] [%s] %s\n", item.Timestamp.Format(time.RFC3339), item.Level, item.Line)
		}
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"code": "ok", "data": paged, "meta": meta})
}

func (s *Server) handleExecutionStream(w http.ResponseWriter, r *http.Request) {
	executionID := r.PathValue("id")
	if _, err := s.repo.GetExecution(r.Context(), executionID); err != nil {
		s.writeError(w, r, http.StatusNotFound, "execution.not_found", nil)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		s.writeError(w, r, http.StatusInternalServerError, "internal.error", nil)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	history, _ := s.repo.ListExecutionLogs(r.Context(), executionID)
	lastID := strings.TrimSpace(r.Header.Get("Last-Event-ID"))
	startIdx := 0
	if lastID != "" {
		if n, err := strconv.Atoi(lastID); err == nil && n > 0 && n < len(history) {
			startIdx = n
		}
	}
	for idx, logRec := range history[startIdx:] {
		eventID := startIdx + idx + 1
		if err := writeSSE(w, "log", eventID, logRec); err != nil {
			return
		}
		flusher.Flush()
	}
	nextID := len(history)

	ch, unsubscribe := s.subscribeExecutionLog(executionID)
	defer unsubscribe()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case logRec := <-ch:
			nextID++
			if err := writeSSE(w, "log", nextID, logRec); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, event string, id int, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "id: %d\n", id); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\n", event); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
		return err
	}
	return nil
}
