package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestWithStaticFallback_ServesAssetAndSPA(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>index</html>"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte("console.log('ok')"), 0o644); err != nil {
		t.Fatalf("write app.js: %v", err)
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("api"))
	})
	h := withStaticFallback(next, dir)

	assetReq := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	assetRes := httptest.NewRecorder()
	h.ServeHTTP(assetRes, assetReq)
	if assetRes.Code != http.StatusOK {
		t.Fatalf("asset status=%d body=%s", assetRes.Code, assetRes.Body.String())
	}
	if got := assetRes.Body.String(); got != "console.log('ok')" {
		t.Fatalf("asset body=%q", got)
	}

	spaReq := httptest.NewRequest(http.MethodGet, "/settings/profile", nil)
	spaRes := httptest.NewRecorder()
	h.ServeHTTP(spaRes, spaReq)
	if spaRes.Code != http.StatusOK {
		t.Fatalf("spa status=%d body=%s", spaRes.Code, spaRes.Body.String())
	}
	if got := spaRes.Body.String(); got != "<html>index</html>" {
		t.Fatalf("spa body=%q", got)
	}

	apiReq := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	apiRes := httptest.NewRecorder()
	h.ServeHTTP(apiRes, apiReq)
	if apiRes.Code != http.StatusTeapot {
		t.Fatalf("api status=%d body=%s", apiRes.Code, apiRes.Body.String())
	}
	b, _ := io.ReadAll(apiRes.Result().Body)
	if string(b) != "api" {
		t.Fatalf("api body=%q", string(b))
	}
}

func TestNormalizeBasePath(t *testing.T) {
	cases := map[string]string{
		"":        "/",
		"/":       "/",
		"api":     "/api",
		"/api":    "/api",
		"/api/":   "/api",
		" /x/y/ ": "/x/y",
	}
	for input, want := range cases {
		if got := normalizeBasePath(input); got != want {
			t.Fatalf("normalizeBasePath(%q)=%q want %q", input, got, want)
		}
	}
}

func TestWithBasePath(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	h := withBasePath("/rana", http.StripPrefix("/rana", next))

	okReq := httptest.NewRequest(http.MethodGet, "/rana/healthz", nil)
	okRes := httptest.NewRecorder()
	h.ServeHTTP(okRes, okReq)
	if okRes.Code != http.StatusNoContent {
		t.Fatalf("expected prefixed request to pass, got %d", okRes.Code)
	}

	missReq := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	missRes := httptest.NewRecorder()
	h.ServeHTTP(missRes, missReq)
	if missRes.Code != http.StatusNotFound {
		t.Fatalf("expected non-prefixed request to 404, got %d", missRes.Code)
	}
}

func TestWithBasePath_PrefixBoundary(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	h := withBasePath("/rana", http.StripPrefix("/rana", next))

	boundaryReq := httptest.NewRequest(http.MethodGet, "/rana-api/healthz", nil)
	boundaryRes := httptest.NewRecorder()
	h.ServeHTTP(boundaryRes, boundaryReq)
	if boundaryRes.Code != http.StatusNotFound {
		t.Fatalf("expected prefix boundary mismatch to 404, got %d", boundaryRes.Code)
	}
}
