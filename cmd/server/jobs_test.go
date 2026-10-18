package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func resetJobStore(t *testing.T) {
	t.Helper()
	jobsMu.Lock()
	jobs = make(map[string]*Job)
	jobOrder = nil
	jobsMu.Unlock()
}

func TestUniqueOutputPath(t *testing.T) {
	dir := t.TempDir()
	first := uniqueOutputPath(dir, "clip_balance.mp4")
	if err := os.WriteFile(first, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := uniqueOutputPath(dir, "clip_balance.mp4")
	if second == first || !strings.HasSuffix(second, "_2.mp4") {
		t.Errorf("uniqueOutputPath = %q, want a _2 suffix", second)
	}
}

func TestScanVideoFiles(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "out")
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.mp4", "b.txt", "sub/c.MKV", "out/ignored.mp4"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files, err := scanVideoFiles(root, out)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(files, " ")
	if !strings.Contains(joined, "a.mp4") || !strings.Contains(joined, "c.MKV") {
		t.Errorf("missing videos: %v", files)
	}
	if strings.Contains(joined, "b.txt") || strings.Contains(joined, "ignored.mp4") {
		t.Errorf("unexpected files: %v", files)
	}
}

func TestBatchOutputPath(t *testing.T) {
	root := t.TempDir()
	out := t.TempDir()
	file := filepath.Join(root, "sub", "movie.mkv")
	got := batchOutputPath(root, file, out, "fast")
	if filepath.Base(got) != "sub_movie_fast.mp4" {
		t.Errorf("batchOutputPath = %q", got)
	}
}

func TestCancelQueuedJob(t *testing.T) {
	resetJobStore(t)
	job := newJob("clip.mp4", "in.mp4", filepath.Join(t.TempDir(), "out.mp4"), "balance", true, 10)
	status, ok := cancelJob(job.ID)
	if !ok || status != "canceled" {
		t.Fatalf("cancelJob = %q, %v", status, ok)
	}
	got, ok := getJob(job.ID)
	if !ok || got.Status != "canceled" {
		t.Errorf("job status = %q, want canceled", got.Status)
	}
	if _, ok := cancelJob("missing"); ok {
		t.Error("cancelJob(missing) should return false")
	}
}

func TestJobsHandlers(t *testing.T) {
	resetJobStore(t)
	job := newJob("clip.mp4", "in.mp4", filepath.Join(t.TempDir(), "out.mp4"), "fast", true, 10)

	rec := httptest.NewRecorder()
	jobsHandler(rec, httptest.NewRequest("GET", "/api/jobs", nil))
	if rec.Code != 200 {
		t.Fatalf("GET /api/jobs = %d", rec.Code)
	}
	var list []Job
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != job.ID {
		t.Errorf("unexpected jobs list: %+v", list)
	}

	rec = httptest.NewRecorder()
	jobItemHandler(rec, httptest.NewRequest("GET", "/api/jobs/"+job.ID, nil))
	if rec.Code != 200 {
		t.Errorf("GET job = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	jobItemHandler(rec, httptest.NewRequest("DELETE", "/api/jobs/"+job.ID, nil))
	if rec.Code != 200 {
		t.Errorf("DELETE job = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	jobItemHandler(rec, httptest.NewRequest("DELETE", "/api/jobs/missing", nil))
	if rec.Code != 404 {
		t.Errorf("DELETE missing = %d, want 404", rec.Code)
	}
}

func TestBatchHandlerValidation(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/batch", strings.NewReader(`{"input_dir":""}`))
	batchHandler(rec, req)
	if rec.Code != 400 {
		t.Errorf("empty input_dir = %d, want 400", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/batch", strings.NewReader(`{"input_dir":"/definitely/not/here"}`))
	batchHandler(rec, req)
	if rec.Code != 400 {
		t.Errorf("missing input_dir = %d, want 400", rec.Code)
	}
}

func TestOutputCollisionConcurrent(t *testing.T) {
	dir := t.TempDir()
	const n = 20
	paths := make(chan string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			paths <- uniqueOutputPath(dir, "video.mp4")
		}()
	}
	wg.Wait()
	close(paths)

	seen := make(map[string]bool)
	for path := range paths {
		if seen[path] {
			t.Errorf("duplicate output path: %s", path)
		}
		seen[path] = true
	}
	if len(seen) != n {
		t.Errorf("unique paths = %d, want %d", len(seen), n)
	}
}

func TestBatchDefaultOutputNextToSource(t *testing.T) {
	resetJobStore(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "video.mp4")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/api/batch",
		strings.NewReader(`{"input_dir":"`+dir+`","profile":"fast"}`))
	rec := httptest.NewRecorder()
	batchHandler(rec, req)
	if rec.Code != 200 {
		t.Fatalf("batch = %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Jobs []jobRef `json:"jobs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Jobs) != 1 {
		t.Fatalf("jobs = %d, want 1", len(resp.Jobs))
	}
	job, ok := getJob(resp.Jobs[0].ID)
	if !ok {
		t.Fatal("job not found")
	}
	if filepath.Dir(job.OutputPath) != dir {
		t.Errorf("output %q, want next to source %q", job.OutputPath, dir)
	}
	select {
	case <-jobQueue:
	default:
	}
}

func TestRunningJobCancel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake ffmpeg is POSIX-only")
	}
	resetJobStore(t)

	dir := t.TempDir()
	fakeFFmpeg := filepath.Join(dir, "fake-ffmpeg.sh")
	if err := os.WriteFile(fakeFFmpeg, []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(dir, "input.mp4")
	if err := os.WriteFile(input, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	oldFFmpeg, oldEncoder := activeFFmpeg, activeEncoder
	activeFFmpeg = fakeFFmpeg
	activeEncoder = EncoderChoice{Name: "libx265", Family: "x265"}
	t.Cleanup(func() {
		activeFFmpeg, activeEncoder = oldFFmpeg, oldEncoder
	})

	job := newJob("video.mp4", input, filepath.Join(dir, "out.mp4"), "fast", true, 1)
	t.Cleanup(func() { cancelJob(job.ID) })

	done := make(chan struct{})
	go func() {
		runJob(job)
		close(done)
	}()

	deadline := time.Now().Add(3 * time.Second)
	for {
		got, _ := getJob(job.ID)
		if got.Status == "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job did not start, status=%q", got.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}

	status, ok := cancelJob(job.ID)
	if !ok || status != "canceled" {
		t.Fatalf("cancelJob = %q, %v", status, ok)
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runJob did not stop after cancellation")
	}

	got, _ := getJob(job.ID)
	if got.Status != "canceled" {
		t.Errorf("final status = %q, want canceled", got.Status)
	}
}
