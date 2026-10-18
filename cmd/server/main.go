package main

import (
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const appVersion = "2.1.2"

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
	bindAddr := envOr("BIND_ADDR", "127.0.0.1")
	authHash := strings.TrimSpace(os.Getenv("BASIC_AUTH_HASH"))
	authUser := envOr("BASIC_AUTH_USER", "admin")

	var handler http.Handler = http.DefaultServeMux
	var err error
	handler, err = basicAuthHandler(handler, authUser, authHash)
	if err != nil {
		log.Fatalf("❌ BASIC_AUTH_HASH: %v", err)
	}
	if !isLoopback(bindAddr) && authHash == "" {
		log.Printf("⚠️ Сервер доступен по сети без пароля. Задайте BASIC_AUTH_HASH.")
	}

	host := browserHost(bindAddr)
	url := "http://" + net.JoinHostPort(host, port)
	if os.Getenv("NO_BROWSER") == "" &&
		(runtime.GOOS == "windows" || runtime.GOOS == "darwin" || os.Getenv("DISPLAY") != "") {
		go openBrowser(url)
	}

	log.Printf("🌐 Слушаю %s (auth: %v)", net.JoinHostPort(bindAddr, port), authHash != "")
	log.Printf("🚀 Сервер: %s", url)
	log.Println("Нажми Ctrl+C для выхода")

	if err := http.ListenAndServe(net.JoinHostPort(bindAddr, port), handler); err != nil {
		log.Fatalf("❌ %v", err)
	}
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func browserHost(bindAddr string) string {
	if isLoopback(bindAddr) || bindAddr == "0.0.0.0" || bindAddr == "::" {
		return "127.0.0.1"
	}
	return bindAddr
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
