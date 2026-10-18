package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResolveOutputFile(t *testing.T) {
	dir := t.TempDir()
	outputDir := filepath.Join(dir, "output")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(outputDir, "video.mp4")
	if err := os.WriteFile(file, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := resolveOutputFile(outputDir, file)
	if err != nil {
		t.Fatalf("resolveOutputFile: %v", err)
	}
	if got != file {
		t.Errorf("resolved %q, want %q", got, file)
	}

	if _, err := resolveOutputFile(outputDir, filepath.Join(outputDir, "missing.mp4")); err == nil {
		t.Error("expected error for missing file")
	}

	outside := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveOutputFile(outputDir, outside); err != errOutsideOutput {
		t.Errorf("outside path error = %v, want errOutsideOutput", err)
	}
	if _, err := resolveOutputFile(outputDir, outputDir); err == nil {
		t.Error("expected error for a directory")
	}

	if runtime.GOOS != "windows" {
		link := filepath.Join(outputDir, "escape.mp4")
		if err := os.Symlink(outside, link); err == nil {
			if _, err := resolveOutputFile(outputDir, link); err != errOutsideOutput {
				t.Errorf("symlink escape error = %v, want errOutsideOutput", err)
			}
		}
	}
}

func TestOpenFolderHandlerValidation(t *testing.T) {
	outputDir := t.TempDir()
	file := filepath.Join(outputDir, "video.mp4")
	if err := os.WriteFile(file, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	handler := openFolderHandler(outputDir)

	tests := []struct {
		name   string
		method string
		body   string
		want   int
	}{
		{"wrong method", http.MethodGet, "", http.StatusMethodNotAllowed},
		{"bad json", http.MethodPost, "{", http.StatusBadRequest},
		{"no path", http.MethodPost, `{}`, http.StatusBadRequest},
		{"outside", http.MethodPost, `{"path":"/etc/passwd"}`, http.StatusForbidden},
		{"missing job", http.MethodPost, `{"jobId":"nope"}`, http.StatusNotFound},
		{"missing file", http.MethodPost, `{"path":"` + filepath.Join(outputDir, "nope.mp4") + `"}`, http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tt.method, "/api/open-folder", strings.NewReader(tt.body))
			handler(rec, req)
			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d; body=%s", rec.Code, tt.want, rec.Body.String())
			}
		})
	}
}

func TestOpenFolderHandlerDocker(t *testing.T) {
	t.Setenv("DOCKER", "true")
	outputDir := t.TempDir()
	file := filepath.Join(outputDir, "video.mp4")
	if err := os.WriteFile(file, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	body := `{"path":"` + file + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/open-folder", strings.NewReader(body))
	openFolderHandler(outputDir)(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["path"] != file {
		t.Errorf("fallback path = %q, want %q", resp["path"], file)
	}
}

func TestOpenFolderHandlerHeadless(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("headless check is Linux-specific")
	}
	if isDockerEnvironment() {
		t.Skip("running inside a container; Docker branch is covered separately")
	}
	t.Setenv("DOCKER", "")
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")

	outputDir := t.TempDir()
	file := filepath.Join(outputDir, "video.mp4")
	if err := os.WriteFile(file, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/open-folder",
		strings.NewReader(`{"path":"`+file+`"}`))
	openFolderHandler(outputDir)(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["path"] != file {
		t.Errorf("fallback path = %q, want %q", resp["path"], file)
	}
}

func TestOpenInFileManagerMissingFile(t *testing.T) {
	if err := openInFileManager(filepath.Join(t.TempDir(), "nope.mp4")); err == nil {
		t.Error("expected error for missing file")
	}
}

func TestDownloadHandler(t *testing.T) {
	outputDir := t.TempDir()
	content := []byte("video-bytes")
	if err := os.WriteFile(filepath.Join(outputDir, "clip.mp4"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	handler := downloadHandler(outputDir)

	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, "/download?file=clip.mp4", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != string(content) {
		t.Errorf("body = %q", rec.Body.String())
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Errorf("Content-Disposition = %q", cd)
	}

	for _, target := range []string{"", "../secret", "..", "missing.mp4"} {
		rec := httptest.NewRecorder()
		handler(rec, httptest.NewRequest(http.MethodGet, "/download?file="+target, nil))
		if rec.Code == http.StatusOK {
			t.Errorf("file=%q unexpectedly returned 200", target)
		}
	}
}
