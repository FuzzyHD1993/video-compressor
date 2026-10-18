package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Job struct {
	ID         string    `json:"id"`
	Filename   string    `json:"filename"`
	Status     string    `json:"status"`
	Progress   int       `json:"progress"`
	OutputName string    `json:"outputName,omitempty"`
	OutputPath string    `json:"outputPath,omitempty"`
	Error      string    `json:"error,omitempty"`
	InputSize  int64     `json:"inputSize"`
	OutputSize int64     `json:"outputSize,omitempty"`
	Profile    string    `json:"profile"`
	CreatedAt  time.Time `json:"createdAt"`

	inputPath string
	keepInput bool
	ctx       context.Context
	cancel    context.CancelFunc
}

var (
	jobsMu   sync.RWMutex
	jobs     = make(map[string]*Job)
	jobOrder []*Job
	jobSeq   atomic.Uint64

	reservedOutputs sync.Map

	jobQueue = make(chan *Job, 100)

	activeFFmpeg  string
	activeEncoder EncoderChoice
)

func newJob(filename, inputPath, outputPath, profile string, keepInput bool, inputSize int64) *Job {
	ctx, cancel := context.WithCancel(context.Background())
	if abs, err := filepath.Abs(outputPath); err == nil {
		outputPath = abs
	}
	id := fmt.Sprintf("job_%d_%d", time.Now().UnixNano(), jobSeq.Add(1))
	job := &Job{
		ID:         id,
		Filename:   filename,
		Status:     "queued",
		Progress:   0,
		OutputName: filepath.Base(outputPath),
		OutputPath: outputPath,
		InputSize:  inputSize,
		Profile:    normalizeProfile(profile),
		CreatedAt:  time.Now(),
		inputPath:  inputPath,
		keepInput:  keepInput,
		ctx:        ctx,
		cancel:     cancel,
	}
	jobsMu.Lock()
	jobs[id] = job
	jobOrder = append(jobOrder, job)
	jobsMu.Unlock()
	return job
}

func enqueueJob(job *Job) bool {
	select {
	case jobQueue <- job:
		return true
	default:
		jobsMu.Lock()
		job.Status = "error"
		job.Error = "очередь переполнена"
		jobsMu.Unlock()
		return false
	}
}

func startWorkers(count int, ffmpegPath string, choice EncoderChoice) {
	activeFFmpeg = ffmpegPath
	activeEncoder = choice
	for i := 0; i < count; i++ {
		go worker()
	}
}

func worker() {
	for job := range jobQueue {
		runJob(job)
	}
}

func runJob(job *Job) {
	jobsMu.Lock()
	if job.Status == "canceled" {
		jobsMu.Unlock()
		cleanupInput(job)
		return
	}
	job.Status = "running"
	job.Progress = 0
	ctx := job.ctx
	jobsMu.Unlock()

	log.Printf("🔄 Начало: %s (%s, %s)", job.Filename, activeEncoder.Name, job.Profile)

	opts := applyEncoder(getOpts(job.Profile), activeEncoder)
	err := compressJob(ctx, job, activeFFmpeg, job.inputPath, job.OutputPath, opts)

	jobsMu.Lock()
	switch {
	case ctx.Err() != nil:
		job.Status = "canceled"
		job.Error = "отменено"
	case err != nil:
		job.Status = "error"
		job.Error = err.Error()
	default:
		job.Status = "done"
		job.Progress = 100
	}
	jobsMu.Unlock()

	if err != nil && ctx.Err() == nil {
		log.Printf("❌ Ошибка: %s: %v", job.Filename, err)
	} else if ctx.Err() != nil {
		log.Printf("⏹ Отменено: %s", job.Filename)
	} else {
		jobsMu.RLock()
		outSize := job.OutputSize
		jobsMu.RUnlock()
		log.Printf("✅ Готово: %s (%.1f МБ → %.1f МБ)",
			job.Filename, float64(job.InputSize)/1024/1024, float64(outSize)/1024/1024)
	}
	cleanupInput(job)
}

func cleanupInput(job *Job) {
	if !job.keepInput && job.inputPath != "" {
		os.Remove(job.inputPath)
	}
}

func compressJob(ctx context.Context, job *Job, ffmpegPath, inputPath, outputPath string, opts Options) error {
	args := buildFFmpegArgs(inputPath, outputPath, opts)
	cmd := exec.CommandContext(ctx, ffmpegPath, args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	var progressMu sync.Mutex
	var duration, currentTime float64
	stdoutDone := make(chan struct{})
	stderrDone := make(chan struct{})

	go func() {
		defer close(stdoutDone)
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
					jobsMu.Lock()
					job.Progress = p
					jobsMu.Unlock()
				}
			}
			if strings.HasPrefix(line, "progress=end") {
				jobsMu.Lock()
				job.Progress = 100
				jobsMu.Unlock()
			}
		}
	}()

	var stderrTail []string
	go func() {
		defer close(stderrDone)
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
	<-stdoutDone
	<-stderrDone
	if ctx.Err() != nil {
		return ctx.Err()
	}

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
		return fmt.Errorf("%s", msg)
	}

	jobsMu.Lock()
	if abs, err := filepath.Abs(outputPath); err == nil {
		job.OutputPath = abs
	}
	job.OutputSize = info.Size()
	jobsMu.Unlock()
	return nil
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

func listJobs() []Job {
	jobsMu.RLock()
	defer jobsMu.RUnlock()
	result := make([]Job, 0, len(jobOrder))
	for i := len(jobOrder) - 1; i >= 0; i-- {
		result = append(result, *jobOrder[i])
	}
	return result
}

func getJob(id string) (Job, bool) {
	jobsMu.RLock()
	defer jobsMu.RUnlock()
	job, ok := jobs[id]
	if !ok {
		return Job{}, false
	}
	return *job, true
}

func cancelJob(id string) (string, bool) {
	jobsMu.Lock()
	defer jobsMu.Unlock()
	job, ok := jobs[id]
	if !ok {
		return "", false
	}
	switch job.Status {
	case "queued", "running":
		if job.cancel != nil {
			job.cancel()
		}
		job.Status = "canceled"
		job.Error = "отменено"
		return "canceled", true
	default:
		return "cannot cancel job in status " + job.Status, true
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

// scanVideoFiles walks inputDir recursively and returns video files, skipping
// outputDir when it is nested inside inputDir.
func scanVideoFiles(inputDir, outputDir string) ([]string, error) {
	absIn, err := filepath.Abs(inputDir)
	if err != nil {
		return nil, err
	}
	absOut, _ := filepath.Abs(outputDir)

	var files []string
	err = filepath.WalkDir(absIn, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if absOut != "" && path == absOut && isWithinDir(absIn, absOut) {
				return filepath.SkipDir
			}
			return nil
		}
		if isVideoExtension(strings.ToLower(filepath.Ext(path))) {
			files = append(files, path)
			if len(files) > 1000 {
				return fmt.Errorf("слишком много видеофайлов (лимит 1000)")
			}
		}
		return nil
	})
	return files, err
}

func uniqueOutputPath(dir, name string) string {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	candidate := filepath.Join(dir, name)
	for i := 1; ; i++ {
		_, reserved := reservedOutputs.Load(candidate)
		_, statErr := os.Stat(candidate)
		if !reserved && os.IsNotExist(statErr) {
			reservedOutputs.Store(candidate, true)
			return candidate
		}
		candidate = filepath.Join(dir, fmt.Sprintf("%s_%d%s", base, i+1, ext))
	}
}

func batchOutputPath(inputRoot, file, outputDir, profile string) string {
	rel, err := filepath.Rel(inputRoot, file)
	if err != nil {
		rel = filepath.Base(file)
	}
	ext := filepath.Ext(rel)
	base := strings.TrimSuffix(rel, ext)
	base = strings.ReplaceAll(base, string(os.PathSeparator), "_")
	return uniqueOutputPath(outputDir, fmt.Sprintf("%s_%s.mp4", base, profile))
}
