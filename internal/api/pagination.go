package api

import (
	"net/http"
	"strconv"
)

const (
	defaultPage     = 1
	defaultPageSize = 20
	maxPageSize     = 200
)

type pageSpec struct {
	Page     int
	PageSize int
}

func parsePageSpec(r *http.Request) pageSpec {
	page := parsePositiveInt(r.URL.Query().Get("page"), defaultPage)
	size := parsePositiveInt(r.URL.Query().Get("page_size"), defaultPageSize)
	if size > maxPageSize {
		size = maxPageSize
	}
	return pageSpec{Page: page, PageSize: size}
}

func parsePositiveInt(raw string, fallback int) int {
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}

func paginate[T any](items []T, spec pageSpec) ([]T, map[string]any) {
	total := len(items)
	if total == 0 {
		return []T{}, map[string]any{
			"page":        spec.Page,
			"page_size":   spec.PageSize,
			"total":       0,
			"total_pages": 0,
		}
	}

	start := (spec.Page - 1) * spec.PageSize
	if start >= total {
		return []T{}, map[string]any{
			"page":        spec.Page,
			"page_size":   spec.PageSize,
			"total":       total,
			"total_pages": (total + spec.PageSize - 1) / spec.PageSize,
		}
	}
	end := start + spec.PageSize
	if end > total {
		end = total
	}
	out := make([]T, end-start)
	copy(out, items[start:end])
	return out, map[string]any{
		"page":        spec.Page,
		"page_size":   spec.PageSize,
		"total":       total,
		"total_pages": (total + spec.PageSize - 1) / spec.PageSize,
	}
}
