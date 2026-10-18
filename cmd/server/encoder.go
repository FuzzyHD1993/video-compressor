package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// EncoderChoice is the best encoder available on this machine.
type EncoderChoice struct {
	Name     string
	Family   string
	InitArgs []string
}

type Options struct {
	VideoCodec string
	Family     string
	InitArgs   []string
	AudioCodec string
	CRF        int
	Bitrate    string
	Width      int
	Height     int
	Preset     string
	Threads    int
	Profile    string
}

var encoderCandidates = []string{
	"hevc_nvenc", "h264_nvenc",
	"hevc_qsv", "h264_qsv",
	"hevc_amf", "h264_amf",
	"libx265", "libx264",
}

// detectBestEncoder probes encoders in priority order: NVENC -> QSV -> AMF ->
// CPU. ENCODER forces a specific encoder, FORCE_CPU=1 skips hardware.
func detectBestEncoder(ffmpegPath string) EncoderChoice {
	available := availableEncoders(ffmpegPath)

	if forceCPU() {
		for _, name := range []string{"libx265", "libx264"} {
			if available[name] {
				return EncoderChoice{Name: name, Family: encoderFamily(name)}
			}
		}
		return EncoderChoice{Name: "libx265", Family: "x265"}
	}

	if forced := strings.TrimSpace(os.Getenv("ENCODER")); forced != "" {
		if !available[forced] {
			log.Printf("⚠️ ENCODER=%s отсутствует в FFmpeg, используется авто-выбор", forced)
		} else if choice, ok := probeChoice(ffmpegPath, forced); ok {
			return choice
		} else {
			log.Printf("⚠️ ENCODER=%s не прошёл тестовый кадр, используется авто-выбор", forced)
		}
	}

	for _, name := range encoderCandidates {
		if !available[name] {
			continue
		}
		if choice, ok := probeChoice(ffmpegPath, name); ok {
			return choice
		}
	}

	log.Printf("⚠️ Аппаратные энкодеры недоступны, используется CPU")
	return EncoderChoice{Name: "libx265", Family: "x265"}
}

func forceCPU() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("FORCE_CPU")))
	return v == "1" || v == "true" || v == "yes"
}

func availableEncoders(ffmpegPath string) map[string]bool {
	out, err := exec.Command(ffmpegPath, "-hide_banner", "-encoders").Output()
	if err != nil {
		return map[string]bool{}
	}
	available := make(map[string]bool)
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			available[fields[1]] = true
		}
	}
	return available
}

func probeChoice(ffmpegPath, name string) (EncoderChoice, bool) {
	family := encoderFamily(name)
	if family == "qsv" {
		if encoderWorks(ffmpegPath, name, nil) {
			return EncoderChoice{Name: name, Family: family}, true
		}
		// Some QSV builds need an explicit device.
		initArgs := []string{"-init_hw_device", "qsv=hw", "-filter_hw_device", "hw"}
		if encoderWorks(ffmpegPath, name, initArgs) {
			return EncoderChoice{Name: name, Family: family, InitArgs: initArgs}, true
		}
		return EncoderChoice{}, false
	}
	if encoderWorks(ffmpegPath, name, nil) {
		return EncoderChoice{Name: name, Family: family}, true
	}
	return EncoderChoice{}, false
}

// encoderWorks performs a tiny test encode. Listing an encoder is not enough:
// the container/driver may be missing even when FFmpeg was built with support.
func encoderWorks(ffmpegPath, encoder string, initArgs []string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	args := []string{"-hide_banner", "-loglevel", "error"}
	args = append(args, initArgs...)
	args = append(args,
		"-f", "lavfi", "-i", "color=size=256x256:rate=1",
		"-frames:v", "1",
		"-c:v", encoder,
		"-f", "null", "-",
	)
	return exec.CommandContext(ctx, ffmpegPath, args...).Run() == nil
}

func encoderFamily(name string) string {
	switch {
	case strings.HasSuffix(name, "_nvenc"):
		return "nvenc"
	case strings.HasSuffix(name, "_qsv"):
		return "qsv"
	case strings.HasSuffix(name, "_amf"):
		return "amf"
	case name == "libx265":
		return "x265"
	case name == "libx264":
		return "x264"
	default:
		return "cpu"
	}
}

func isGPUEncoder(name string) bool {
	f := encoderFamily(name)
	return f == "nvenc" || f == "qsv" || f == "amf"
}

func normalizeProfile(profile string) string {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "fast", "low":
		return "fast"
	case "quality", "high":
		return "quality"
	default:
		return "balance"
	}
}

func getOpts(profile string) Options {
	profile = normalizeProfile(profile)
	opts := Options{
		AudioCodec: "aac",
		Profile:    profile,
		Threads:    defaultThreads(),
	}
	switch profile {
	case "fast":
		opts.CRF = 30
		opts.Bitrate = "96k"
	case "quality":
		opts.CRF = 20
		opts.Bitrate = "192k"
	default:
		opts.CRF = 26
		opts.Bitrate = "128k"
	}
	return opts
}

func defaultThreads() int {
	n := runtime.NumCPU()
	switch {
	case n <= 4:
		return 2
	case n <= 8:
		return 4
	default:
		return 8
	}
}

func applyEncoder(opts Options, choice EncoderChoice) Options {
	opts.VideoCodec = choice.Name
	opts.Family = choice.Family
	opts.InitArgs = choice.InitArgs
	switch choice.Family {
	case "nvenc":
		opts.Preset = nvencPreset(opts.Profile)
	case "qsv":
		opts.Preset = qsvPreset(opts.Profile)
	case "amf":
		opts.Preset = amfPreset(opts.Profile)
	case "x265", "x264":
		opts.Preset = cpuPreset(opts.Profile)
	}
	return opts
}

func nvencPreset(profile string) string {
	switch normalizeProfile(profile) {
	case "fast":
		return "p2"
	case "quality":
		return "p6"
	default:
		return "p4"
	}
}

func qsvPreset(profile string) string {
	switch normalizeProfile(profile) {
	case "fast":
		return "veryfast"
	case "quality":
		return "slow"
	default:
		return "medium"
	}
}

func amfPreset(profile string) string {
	switch normalizeProfile(profile) {
	case "fast":
		return "speed"
	case "quality":
		return "quality"
	default:
		return "balanced"
	}
}

func cpuPreset(profile string) string {
	switch normalizeProfile(profile) {
	case "fast":
		return "ultrafast"
	case "quality":
		return "slow"
	default:
		return "medium"
	}
}

// buildFFmpegArgs builds the ffmpeg command line for the selected encoder.
func buildFFmpegArgs(inputPath, outputPath string, opts Options) []string {
	args := []string{"-hide_banner", "-y"}
	args = append(args, opts.InitArgs...)
	args = append(args, "-i", inputPath, "-c:v", opts.VideoCodec)

	switch opts.Family {
	case "nvenc":
		args = append(args,
			"-preset", opts.Preset,
			"-rc", "vbr",
			"-cq", strconv.Itoa(opts.CRF),
			"-b:v", "0",
		)
	case "qsv":
		args = append(args,
			"-preset", opts.Preset,
			"-global_quality", strconv.Itoa(opts.CRF),
		)
	case "amf":
		args = append(args,
			"-quality", opts.Preset,
			"-rc", "cqp",
			"-qp_i", strconv.Itoa(opts.CRF),
			"-qp_p", strconv.Itoa(opts.CRF),
			"-qp_b", strconv.Itoa(opts.CRF),
		)
	default:
		args = append(args,
			"-preset", opts.Preset,
			"-crf", strconv.Itoa(opts.CRF),
		)
		if opts.Threads > 0 {
			args = append(args, "-threads", strconv.Itoa(opts.Threads))
		}
	}

	if opts.Width > 0 && opts.Height > 0 {
		args = append(args, "-vf", scaleFilter(opts.Width, opts.Height))
	}
	if strings.Contains(opts.VideoCodec, "hevc") {
		args = append(args, "-tag:v", "hvc1")
	}

	args = append(args,
		"-c:a", opts.AudioCodec,
		"-b:a", opts.Bitrate,
		"-movflags", "+faststart",
		"-progress", "pipe:1",
		"-nostats",
		outputPath,
	)
	return args
}

func scaleFilter(width, height int) string {
	return fmt.Sprintf(
		"scale=%d:%d:force_original_aspect_ratio=decrease:force_divisible_by=2",
		width, height,
	)
}
