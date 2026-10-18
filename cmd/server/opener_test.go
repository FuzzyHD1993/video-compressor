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
	old := isDockerFunction
	isDockerFunction = func() bool { return true }
	t.Cleanup(func() { isDockerFunction = old })
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")

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

func TestOpenFolderJobOutsideOutput(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("headless/open behavior is Linux-specific")
	}
	old := isDockerFunction
	isDockerFunction = func() bool { return false }
	t.Cleanup(func() { isDockerFunction = old })
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	resetJobStore(t)

	sourceDir := t.TempDir()
	out := filepath.Join(sourceDir, "movie_fast.mp4")
	if err := os.WriteFile(out, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	job := newJob("movie.mp4", filepath.Join(sourceDir, "movie.mp4"), out, "fast", true, 1)
	jobsMu.Lock()
	job.Status = "done"
	job.OutputPath = out
	jobsMu.Unlock()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/open-folder", strings.NewReader(`{"jobId":"`+job.ID+`"}`))
	openFolderHandler(t.TempDir())(rec, req)
	// Headless environment: must fail with the path for fallback, not 403.
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["path"] != out {
		t.Errorf("fallback path = %q, want %q", resp["path"], out)
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

func TestDownloadByJobIDOutsideOutput(t *testing.T) {
	resetJobStore(t)
	sourceDir := t.TempDir()
	out := filepath.Join(sourceDir, "movie_fast.mp4")
	content := []byte("batch-output")
	if err := os.WriteFile(out, content, 0o644); err != nil {
		t.Fatal(err)
	}
	job := newJob("movie.mp4", filepath.Join(sourceDir, "movie.mp4"), out, "fast", true, 1)
	jobsMu.Lock()
	job.Status = "done"
	job.OutputPath = out
	jobsMu.Unlock()

	rec := httptest.NewRecorder()
	downloadHandler(t.TempDir())(rec, httptest.NewRequest("GET", "/download?jobId="+job.ID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("job download = %d, want 200", rec.Code)
	}
	if rec.Body.String() != string(content) {
		t.Errorf("body = %q", rec.Body.String())
	}
}
