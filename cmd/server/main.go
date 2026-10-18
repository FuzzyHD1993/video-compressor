package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Job struct {
	ID         string `json:"id"`
	Filename   string `json:"filename"`
	Status     string `json:"status"`
	Progress   int    `json:"progress"`
	OutputName string `json:"outputName,omitempty"`
	OutputPath string `json:"outputPath,omitempty"`
	Error      string `json:"error,omitempty"`
	InputSize  int64  `json:"inputSize"`
	OutputSize int64  `json:"outputSize,omitempty"`
}

type Options struct {
	VideoCodec string
	AudioCodec string
	CRF        int
	Bitrate    string
	Width      int
	Height     int
	Preset     string
}

var (
	jobQueue  = make(map[string]*Job)
	jobMutex  sync.RWMutex
	semaphore chan struct{}
)

const appVersion = "1.2.2"

func main() {
	prepareWorkingDir()

	ffmpegPath := findFFmpeg()
	if ffmpegPath == "" {
		log.Fatal("❌ FFmpeg не найден!")
	}
	log.Printf("✅ FFmpeg найден: %s", ffmpegPath)

	encoder := getEncoder(ffmpegPath)
	workers := workerCount(encoder)
	semaphore = make(chan struct{}, workers)

	log.Printf("⚡ Кодек: %s", encoder)
	if isGPUEncoder(encoder) {
		log.Printf("🚀 GPU-ускорение включено (NVENC/AMF)")
	} else {
		log.Printf("🖥 CPU-кодирование (GPU-энкодер не найден)")
	}
	log.Printf("⚡ Параллельных задач: %d", workers)

	os.MkdirAll("./temp", 0755)
	os.MkdirAll("./output", 0755)

	http.HandleFunc("/", serveIndex)
	http.HandleFunc("/upload", uploadHandler(ffmpegPath, encoder))
	http.HandleFunc("/status", statusHandler)
	http.HandleFunc("/health", healthHandler(encoder))
	http.HandleFunc("/download", downloadHandler("./output"))
	http.HandleFunc("/api/open-folder", openFolderHandler("./output"))

	port := envOr("PORT", "8095")
	url := "http://localhost:" + port
	if os.Getenv("NO_BROWSER") == "" && os.Getenv("DISPLAY") != "" {
		go openBrowser(url)
	}

	log.Printf("🚀 Сервер: %s", url)
	log.Println("Нажми Ctrl+C для выхода")

	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("❌ %v", err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// prepareWorkingDir makes a double-clicked Windows executable keep temp/ and
// output/ next to itself instead of in the process's inherited working
// directory (which can be C:\Windows\System32 for shortcuts).
func prepareWorkingDir() {
	if runtime.GOOS != "windows" {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if err := os.Chdir(filepath.Dir(exe)); err != nil {
		log.Printf("⚠️ Не удалось перейти в папку программы: %v", err)
	}
}

func healthHandler(encoder string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "ok",
			"encoder": encoder,
			"version": appVersion,
		})
	}
}

func findFFmpeg() string {
	if path, err := extractEmbeddedFFmpeg(); err != nil {
		log.Printf("⚠️ Не удалось распаковать встроенный FFmpeg: %v", err)
	} else if path != "" {
		return path
	}
	if path, err := exec.LookPath("ffmpeg"); err == nil {
		return path
	}
	exe, _ := os.Executable()
	dir := filepath.Dir(exe)
	paths := []string{
		filepath.Join(dir, "ffmpeg", "bin", "ffmpeg.exe"),
		filepath.Join(dir, "ffmpeg", "ffmpeg.exe"),
		filepath.Join(dir, "ffmpeg.exe"),
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// getEncoder selects the best available encoder. NVENC is preferred, then
// platform-specific hardware encoders, with libx265 as the CPU fallback.
// Set FORCE_CPU=1 to always use software encoding.
func getEncoder(ffmpegPath string) string {
	if strings.EqualFold(os.Getenv("FORCE_CPU"), "1") ||
		strings.EqualFold(os.Getenv("FORCE_CPU"), "true") {
		return "libx265"
	}

	out, err := exec.Command(ffmpegPath, "-hide_banner", "-encoders").Output()
	if err != nil {
		return "libx265"
	}
	available := make(map[string]bool)
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			available[fields[1]] = true
		}
	}

	candidates := []string{"hevc_nvenc", "h264_nvenc"}
	if runtime.GOOS == "windows" {
		candidates = append(candidates, "hevc_amf", "h264_amf")
	}
	for _, enc := range candidates {
		if available[enc] && encoderWorks(ffmpegPath, enc) {
			return enc
		}
	}
	return "libx265"
}

// encoderWorks performs a tiny test encode. Listing an encoder is not enough:
// an image may include NVENC while the container has no GPU device/driver
// libraries, in which case ffmpeg fails only when a real job starts.
func encoderWorks(ffmpegPath, encoder string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpegPath,
		"-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "color=size=256x256:rate=1",
		"-frames:v", "1",
		"-c:v", encoder,
		"-f", "null", "-",
	)
	return cmd.Run() == nil
}

func isGPUEncoder(encoder string) bool {
	return strings.HasSuffix(encoder, "_nvenc") || strings.HasSuffix(encoder, "_amf")
}

func workerCount(encoder string) int {
	if v := os.Getenv("MAX_WORKERS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	// Consumer GPUs have a limited number of concurrent NVENC sessions, so
	// running one job per CPU core would only queue work inside the driver.
	if isGPUEncoder(encoder) {
		return 2
	}
	return runtime.NumCPU()
}

func uploadHandler(ffmpegPath, encoder string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Только POST", http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseMultipartForm(2 << 30); err != nil {
			http.Error(w, "Слишком большой файл", http.StatusBadRequest)
			return
		}
		file, header, err := r.FormFile("video")
		if err != nil {
			http.Error(w, "Ошибка получения файла", http.StatusBadRequest)
			return
		}
		defer file.Close()

		ext := strings.ToLower(filepath.Ext(header.Filename))
		if !isVideoExtension(ext) {
			http.Error(w, "Неподдерживаемый формат", http.StatusBadRequest)
			return
		}

		timestamp := time.Now().UnixNano()
		inputPath := filepath.Join("./temp", fmt.Sprintf("input_%d%s", timestamp, ext))

		outFile, err := os.Create(inputPath)
		if err != nil {
			http.Error(w, "Ошибка сохранения", http.StatusInternalServerError)
			return
		}
		written, err := io.Copy(outFile, file)
		outFile.Close()
		if err != nil {
			http.Error(w, "Ошибка копирования", http.StatusInternalServerError)
			return
		}

		quality := r.FormValue("quality")
		if quality == "" {
			quality = "medium"
		}
		opts := getOpts(quality)
		opts.VideoCodec = encoder

		outputName := fmt.Sprintf("%s_%s.mp4", strings.TrimSuffix(header.Filename, ext), quality)
		outputPath := filepath.Join("./output", outputName)

		jobID := fmt.Sprintf("job_%d", timestamp)
		job := &Job{
			ID:         jobID,
			Filename:   header.Filename,
			Status:     "pending",
			Progress:   0,
			OutputName: outputName,
			InputSize:  written,
		}

		jobMutex.Lock()
		jobQueue[jobID] = job
		jobMutex.Unlock()

		go func() {
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			processJob(job, ffmpegPath, inputPath, outputPath, opts)
		}()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"jobId": jobID})
	}
}

// buildFFmpegArgs builds the ffmpeg command line for the selected encoder.
func buildFFmpegArgs(inputPath, outputPath string, opts Options) []string {
	args := []string{
		"-hide_banner",
		"-y",
		"-i", inputPath,
		"-c:v", opts.VideoCodec,
	}

	switch {
	case strings.HasSuffix(opts.VideoCodec, "_nvenc"):
		args = append(args,
			"-preset", nvencPreset(opts.Preset),
			"-rc", "vbr",
			"-cq", strconv.Itoa(opts.CRF),
			"-b:v", "0",
		)
	case strings.HasSuffix(opts.VideoCodec, "_amf"):
		args = append(args,
			"-quality", amfPreset(opts.Preset),
			"-rc", "cqp",
			"-qp_i", strconv.Itoa(opts.CRF),
			"-qp_p", strconv.Itoa(opts.CRF),
			"-qp_b", strconv.Itoa(opts.CRF),
		)
	default:
		args = append(args,
			"-crf", strconv.Itoa(opts.CRF),
			"-preset", opts.Preset,
		)
	}

	args = append(args,
		"-vf", scaleFilter(opts.Width, opts.Height),
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

// nvencPreset maps CPU-style presets to NVENC p1 (fastest) .. p7 (best).
func nvencPreset(preset string) string {
	switch preset {
	case "slow", "slower", "veryslow":
		return "p6"
	case "fast", "faster", "veryfast":
		return "p2"
	default:
		return "p4"
	}
}

func amfPreset(preset string) string {
	switch preset {
	case "slow", "slower", "veryslow":
		return "quality"
	case "fast", "faster", "veryfast":
		return "speed"
	default:
		return "balanced"
	}
}

func processJob(job *Job, ffmpegPath, inputPath, outputPath string, opts Options) {
	jobMutex.Lock()
	job.Status = "processing"
	jobMutex.Unlock()

	log.Printf("🔄 Начало: %s (%s)", job.Filename, opts.VideoCodec)

	args := buildFFmpegArgs(inputPath, outputPath, opts)
	cmd := exec.Command(ffmpegPath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		failJob(job, err)
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		failJob(job, err)
		return
	}
	if err := cmd.Start(); err != nil {
		failJob(job, err)
		os.Remove(inputPath)
		return
	}

	var progressMu sync.Mutex
	var duration float64
	var currentTime float64

	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "out_time_ms=") {
				ms, _ := strconv.ParseFloat(strings.TrimPrefix(line, "out_time_ms="), 64)
				progressMu.Lock()
				currentTime = ms / 1000000
				p := progressPercent(currentTime, duration)
				progressMu.Unlock()
				if p >= 0 {
					jobMutex.Lock()
					job.Progress = p
					jobMutex.Unlock()
				}
			}
			if strings.HasPrefix(line, "progress=end") {
				jobMutex.Lock()
				job.Progress = 100
				jobMutex.Unlock()
			}
		}
	}()

	var stderrTail []string
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			stderrTail = append(stderrTail, line)
			if len(stderrTail) > 20 {
				stderrTail = stderrTail[1:]
			}
			if strings.Contains(line, "Duration:") {
				if d, ok := parseDuration(line); ok {
					progressMu.Lock()
					duration = d
					progressMu.Unlock()
				}
			}
		}
	}()

	waitErr := cmd.Wait()

	info, statErr := os.Stat(outputPath)
	if waitErr != nil || statErr != nil {
		msg := "ffmpeg завершился с ошибкой"
		if waitErr != nil {
			msg = waitErr.Error()
		} else if statErr != nil {
			msg = statErr.Error()
		}
		if len(stderrTail) > 0 {
			msg += ": " + strings.Join(stderrTail, " | ")
		}
		failJob(job, fmt.Errorf("%s", msg))
		os.Remove(inputPath)
		return
	}

	jobMutex.Lock()
	job.OutputSize = info.Size()
	job.Status = "done"
	job.Progress = 100
	if abs, err := filepath.Abs(outputPath); err == nil {
		job.OutputPath = abs
	}
	jobMutex.Unlock()
	log.Printf("✅ Готово: %s (%.1f МБ → %.1f МБ)",
		job.Filename, float64(job.InputSize)/1024/1024, float64(info.Size())/1024/1024)
	os.Remove(inputPath)
}

func progressPercent(current, duration float64) int {
	if duration <= 0 || current <= 0 {
		return -1
	}
	p := int((current / duration) * 100)
	if p > 100 {
		p = 100
	}
	return p
}

func parseDuration(line string) (float64, bool) {
	parts := strings.Split(line, "Duration: ")
	if len(parts) < 2 {
		return 0, false
	}
	dur := strings.Split(parts[1], ",")[0]
	durParts := strings.Split(strings.TrimSpace(dur), ":")
	if len(durParts) != 3 {
		return 0, false
	}
	h, _ := strconv.ParseFloat(durParts[0], 64)
	m, _ := strconv.ParseFloat(durParts[1], 64)
	s, _ := strconv.ParseFloat(durParts[2], 64)
	return h*3600 + m*60 + s, true
}

func failJob(job *Job, err error) {
	jobMutex.Lock()
	job.Status = "error"
	job.Error = err.Error()
	jobMutex.Unlock()
	log.Printf("❌ Ошибка: %s: %v", job.Filename, err)
}

func getOpts(quality string) Options {
	switch quality {
	case "high":
		return Options{
			AudioCodec: "aac",
			CRF:        20,
			Bitrate:    "192k",
			Width:      1920,
			Height:     1080,
			Preset:     "slow",
		}
	case "medium":
		return Options{
			AudioCodec: "aac",
			CRF:        23,
			Bitrate:    "128k",
			Width:      1280,
			Height:     720,
			Preset:     "medium",
		}
	default:
		return Options{
			AudioCodec: "aac",
			CRF:        28,
			Bitrate:    "96k",
			Width:      854,
			Height:     480,
			Preset:     "fast",
		}
	}
}

func isVideoExtension(ext string) bool {
	valid := []string{
		".mp4", ".avi", ".mov", ".mkv", ".wmv", ".flv",
		".webm", ".m4v", ".mpg", ".mpeg", ".3gp", ".ogv",
		".ts", ".mts", ".m2ts", ".vob", ".divx",
	}
	for _, v := range valid {
		if ext == v {
			return true
		}
	}
	return false
}

func downloadHandler(outputDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "Только GET", http.StatusMethodNotAllowed)
			return
		}
		name := r.URL.Query().Get("file")
		if name == "" {
			http.Error(w, "file required", http.StatusBadRequest)
			return
		}
		if filepath.Base(name) != name || name == "." || name == ".." {
			http.Error(w, "Недопустимое имя файла", http.StatusBadRequest)
			return
		}
		path := filepath.Join(outputDir, name)
		resolved, err := resolveOutputFile(outputDir, path)
		if err != nil {
			http.Error(w, "Файл не найден", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{
			"filename": name,
		}))
		http.ServeFile(w, r, resolved)
	}
}

func statusHandler(w http.ResponseWriter, r *http.Request) {
	jobID := r.URL.Query().Get("jobId")
	if jobID == "" {
		http.Error(w, "jobId required", http.StatusBadRequest)
		return
	}
	jobMutex.RLock()
	job, exists := jobQueue[jobID]
	jobMutex.RUnlock()
	if !exists {
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(job)
}

func openBrowser(url string) {
	time.Sleep(500 * time.Millisecond)
	switch runtime.GOOS {
	case "windows":
		exec.Command("cmd", "/c", "start", url).Start()
	case "darwin":
		exec.Command("open", url).Start()
	default:
		exec.Command("xdg-open", url).Start()
	}
}
