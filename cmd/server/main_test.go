package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuildFFmpegArgsNVENC(t *testing.T) {
	opts := applyEncoder(getOpts("balance"), EncoderChoice{Name: "hevc_nvenc", Family: "nvenc"})
	args := buildFFmpegArgs("input.mkv", "output.mp4", opts)
	joined := strings.Join(args, " ")

	for _, want := range []string{
		"-c:v hevc_nvenc",
		"-preset p4",
		"-rc vbr",
		"-cq 26",
		"-b:v 0",
		"-tag:v hvc1",
		"-c:a aac",
		"-b:a 128k",
		"-movflags +faststart",
		"-progress pipe:1",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q: %s", want, joined)
		}
	}
	if strings.Contains(joined, "-crf") {
		t.Errorf("NVENC args must not contain -crf: %s", joined)
	}
	if strings.Contains(joined, "scale=") {
		t.Errorf("v2 must not rescale by default: %s", joined)
	}
	if args[len(args)-1] != "output.mp4" {
		t.Errorf("output path should be last, got %q", args[len(args)-1])
	}
}

func TestBuildFFmpegArgsQSV(t *testing.T) {
	opts := applyEncoder(getOpts("fast"), EncoderChoice{Name: "hevc_qsv", Family: "qsv"})
	joined := strings.Join(buildFFmpegArgs("in.mp4", "out.mp4", opts), " ")
	for _, want := range []string{
		"-c:v hevc_qsv",
		"-preset veryfast",
		"-global_quality 30",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q: %s", want, joined)
		}
	}
}

func TestBuildFFmpegArgsAMF(t *testing.T) {
	opts := applyEncoder(getOpts("quality"), EncoderChoice{Name: "hevc_amf", Family: "amf"})
	joined := strings.Join(buildFFmpegArgs("in.mp4", "out.mp4", opts), " ")
	for _, want := range []string{
		"-c:v hevc_amf",
		"-quality quality",
		"-rc cqp",
		"-qp_i 20",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q: %s", want, joined)
		}
	}
}

func TestBuildFFmpegArgsCPU(t *testing.T) {
	opts := applyEncoder(getOpts("fast"), EncoderChoice{Name: "libx265", Family: "x265"})
	opts.Threads = 2
	joined := strings.Join(buildFFmpegArgs("in.avi", "out.mp4", opts), " ")
	for _, want := range []string{
		"-c:v libx265",
		"-crf 30",
		"-preset ultrafast",
		"-threads 2",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q: %s", want, joined)
		}
	}
	if strings.Contains(joined, "-cq") || strings.Contains(joined, "-rc vbr") {
		t.Errorf("CPU args must not contain hardware rate-control flags: %s", joined)
	}
}

func TestBuildFFmpegArgsScaling(t *testing.T) {
	opts := applyEncoder(getOpts("balance"), EncoderChoice{Name: "libx264", Family: "x264"})
	opts.Width, opts.Height = 1280, 720
	joined := strings.Join(buildFFmpegArgs("in.mp4", "out.mp4", opts), " ")
	if !strings.Contains(joined, "scale=1280:720") {
		t.Errorf("expected optional scale filter: %s", joined)
	}
}

func TestPresets(t *testing.T) {
	cases := []struct {
		fn    func(string) string
		value string
		want  string
	}{
		{nvencPreset, "fast", "p2"},
		{nvencPreset, "balance", "p4"},
		{nvencPreset, "quality", "p6"},
		{qsvPreset, "fast", "veryfast"},
		{qsvPreset, "balance", "medium"},
		{qsvPreset, "quality", "slow"},
		{amfPreset, "fast", "speed"},
		{amfPreset, "balance", "balanced"},
		{amfPreset, "quality", "quality"},
		{cpuPreset, "fast", "ultrafast"},
		{cpuPreset, "balance", "medium"},
		{cpuPreset, "quality", "slow"},
	}
	for _, c := range cases {
		if got := c.fn(c.value); got != c.want {
			t.Errorf("preset(%q) = %q, want %q", c.value, got, c.want)
		}
	}
}

func TestEncoderFamilyAndGPU(t *testing.T) {
	for name, family := range map[string]string{
		"hevc_nvenc": "nvenc", "h264_nvenc": "nvenc",
		"hevc_qsv": "qsv", "h264_qsv": "qsv",
		"hevc_amf": "amf", "h264_amf": "amf",
		"libx265": "x265", "libx264": "x264",
	} {
		if got := encoderFamily(name); got != family {
			t.Errorf("encoderFamily(%q) = %q, want %q", name, got, family)
		}
	}
	if !isGPUEncoder("hevc_qsv") || isGPUEncoder("libx265") {
		t.Error("isGPUEncoder is wrong")
	}
}

func TestNormalizeProfile(t *testing.T) {
	cases := map[string]string{
		"fast": "fast", "low": "fast", "quality": "quality", "high": "quality",
		"balance": "balance", "medium": "balance", "": "balance", "wat": "balance",
	}
	for in, want := range cases {
		if got := normalizeProfile(in); got != want {
			t.Errorf("normalizeProfile(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGetOpts(t *testing.T) {
	fast := getOpts("fast")
	if fast.CRF != 30 || fast.Bitrate != "96k" || fast.Profile != "fast" {
		t.Errorf("unexpected fast options: %+v", fast)
	}
	balance := getOpts("balance")
	if balance.CRF != 26 || balance.Bitrate != "128k" {
		t.Errorf("unexpected balance options: %+v", balance)
	}
	quality := getOpts("high")
	if quality.CRF != 20 || quality.Bitrate != "192k" || quality.Profile != "quality" {
		t.Errorf("unexpected quality options: %+v", quality)
	}
}

func TestParseDuration(t *testing.T) {
	got, ok := parseDuration("  Duration: 01:02:03.50, start: 0.000000, bitrate: 1234 kb/s")
	if !ok {
		t.Fatal("expected duration to parse")
	}
	if got != 3723.5 {
		t.Errorf("parseDuration = %v, want 3723.5", got)
	}
	if _, ok := parseDuration("not a duration"); ok {
		t.Error("expected invalid line to fail")
	}
}

func TestProgressPercent(t *testing.T) {
	if got := progressPercent(30, 60); got != 50 {
		t.Errorf("progressPercent(30,60) = %d, want 50", got)
	}
	if got := progressPercent(120, 60); got != 100 {
		t.Errorf("progressPercent should clamp to 100, got %d", got)
	}
	if got := progressPercent(10, 0); got != -1 {
		t.Errorf("progressPercent with unknown duration = %d, want -1", got)
	}
}

func TestScaleFilter(t *testing.T) {
	if got := scaleFilter(1920, 1080); !strings.Contains(got, "scale=1920:1080") {
		t.Errorf("unexpected scale filter: %s", got)
	}
}

func TestEmbeddedIndexHTML(t *testing.T) {
	if len(indexHTML) == 0 {
		t.Fatal("embedded index.html is empty")
	}
	rec := httptest.NewRecorder()
	serveIndex(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 200 {
		t.Errorf("GET / = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Компрессор") {
		t.Error("embedded index does not look like the UI")
	}
}

func TestHealthHandler(t *testing.T) {
	rec := httptest.NewRecorder()
	healthHandler("hevc_nvenc")(rec, httptest.NewRequest("GET", "/health", nil))
	if rec.Code != 200 {
		t.Fatalf("GET /health = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`"status":"ok"`, `"encoder":"hevc_nvenc"`, `"version":"` + appVersion + `"`} {
		if !strings.Contains(body, want) {
			t.Errorf("health body missing %s: %s", want, body)
		}
	}
}

func TestBindHelpers(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "::1", "localhost", "LOCALHOST"} {
		if !isLoopback(host) {
			t.Errorf("isLoopback(%q) = false, want true", host)
		}
	}
	for _, host := range []string{"0.0.0.0", "192.168.0.5", ""} {
		if isLoopback(host) {
			t.Errorf("isLoopback(%q) = true, want false", host)
		}
	}
	if got := browserHost("0.0.0.0"); got != "127.0.0.1" {
		t.Errorf("browserHost(0.0.0.0) = %q", got)
	}
	if got := browserHost("192.168.0.5"); got != "192.168.0.5" {
		t.Errorf("browserHost(LAN) = %q", got)
	}
}
