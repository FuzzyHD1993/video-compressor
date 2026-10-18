package main

import (
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
)

const appVersion = "2.0.0"

func main() {
	prepareWorkingDir()

	ffmpegPath := findFFmpeg()
	if ffmpegPath == "" {
		log.Fatal("❌ FFmpeg не найден!")
	}
	log.Printf("✅ FFmpeg найден: %s", ffmpegPath)

	encoder := detectBestEncoder(ffmpegPath)
	workers := workerCount(encoder)
	log.Printf("⚡ Выбран энкодер: %s (%s)", encoder.Name, encoder.Family)
	if isGPUEncoder(encoder.Name) {
		log.Printf("🚀 Аппаратное ускорение включено")
	} else {
		log.Printf("🖥 CPU-кодирование (аппаратный энкодер не найден)")
	}
	log.Printf("⚡ Параллельных задач: %d", workers)

	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		log.Fatalf("❌ temp: %v", err)
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		log.Fatalf("❌ output: %v", err)
	}

	startWorkers(workers, ffmpegPath, encoder)

	http.HandleFunc("/", serveIndex)
	http.HandleFunc("/upload", uploadHandler)
	http.HandleFunc("/status", statusHandler)
	http.HandleFunc("/health", healthHandler(encoder.Name))
	http.HandleFunc("/download", downloadHandler(outputDir))
	http.HandleFunc("/api/jobs", jobsHandler)
	http.HandleFunc("/api/jobs/", jobItemHandler)
	http.HandleFunc("/api/batch", batchHandler)
	http.HandleFunc("/api/open-folder", openFolderHandler(outputDir))

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

func workerCount(encoder EncoderChoice) int {
	if v := os.Getenv("MAX_WORKERS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	if isGPUEncoder(encoder.Name) {
		return 2
	}
	if runtime.NumCPU() <= 4 {
		return 1
	}
	return 2
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
