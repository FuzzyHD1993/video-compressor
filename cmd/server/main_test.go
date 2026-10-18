package main

import (
	"strings"
	"testing"
)

func TestBuildFFmpegArgsNVENC(t *testing.T) {
	opts := getOpts("medium")
	opts.VideoCodec = "hevc_nvenc"

	args := buildFFmpegArgs("input.mkv", "output.mp4", opts)
	joined := strings.Join(args, " ")

	for _, want := range []string{
		"-c:v hevc_nvenc",
		"-preset p4",
		"-rc vbr",
		"-cq 23",
		"-b:v 0",
		"scale=1280:720:force_original_aspect_ratio=decrease:force_divisible_by=2",
		"-c:a aac",
		"-b:a 128k",
		"-movflags +faststart",
		"-progress pipe:1",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q: %s", want, joined)
		}
	}

	if args[len(args)-1] != "output.mp4" {
		t.Errorf("output path should be last, got %q", args[len(args)-1])
	}
	if strings.Contains(joined, "-crf") {
		t.Errorf("NVENC args must not contain -crf: %s", joined)
	}
}

func TestBuildFFmpegArgsCPU(t *testing.T) {
	opts := getOpts("low")
	opts.VideoCodec = "libx265"

	joined := strings.Join(buildFFmpegArgs("in.avi", "out.mp4", opts), " ")

	for _, want := range []string{
		"-c:v libx265",
		"-crf 28",
		"-preset fast",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q: %s", want, joined)
		}
	}
	if strings.Contains(joined, "-cq") || strings.Contains(joined, "-rc vbr") {
		t.Errorf("CPU args must not contain NVENC rate-control flags: %s", joined)
	}
}

func TestBuildFFmpegArgsAMF(t *testing.T) {
	opts := getOpts("high")
	opts.VideoCodec = "hevc_amf"

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

func TestNVENCPluginPresets(t *testing.T) {
	cases := map[string]string{
		"slow":     "p6",
		"medium":   "p4",
		"fast":     "p2",
		"veryslow": "p6",
		"veryfast": "p2",
		"unknown":  "p4",
		"":         "p4",
	}
	for preset, want := range cases {
		if got := nvencPreset(preset); got != want {
			t.Errorf("nvencPreset(%q) = %q, want %q", preset, got, want)
		}
	}
}

func TestGPUEncoderDetection(t *testing.T) {
	for _, enc := range []string{"hevc_nvenc", "h264_nvenc", "hevc_amf", "h264_amf"} {
		if !isGPUEncoder(enc) {
			t.Errorf("isGPUEncoder(%q) = false, want true", enc)
		}
	}
	for _, enc := range []string{"libx265", "libx264", "hevc_vaapi"} {
		if isGPUEncoder(enc) {
			t.Errorf("isGPUEncoder(%q) = true, want false", enc)
		}
	}
}

func TestGetOpts(t *testing.T) {
	high := getOpts("high")
	if high.Width != 1920 || high.Height != 1080 || high.CRF != 20 {
		t.Errorf("unexpected high options: %+v", high)
	}
	medium := getOpts("medium")
	if medium.Width != 1280 || medium.Height != 720 || medium.CRF != 23 {
		t.Errorf("unexpected medium options: %+v", medium)
	}
	low := getOpts("anything-else")
	if low.Width != 854 || low.Height != 480 || low.CRF != 28 {
		t.Errorf("unexpected low options: %+v", low)
	}
}

func TestParseDuration(t *testing.T) {
	got, ok := parseDuration("  Duration: 01:02:03.50, start: 0.000000, bitrate: 1234 kb/s")
	if !ok {
		t.Fatal("expected duration to parse")
	}
	want := 3723.5
	if got != want {
		t.Errorf("parseDuration = %v, want %v", got, want)
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
	got := scaleFilter(1920, 1080)
	if !strings.Contains(got, "scale=1920:1080") {
		t.Errorf("unexpected scale filter: %s", got)
	}
}
