package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var errOutsideOutput = errors.New("путь вне папки output")

type openFolderRequest struct {
	Path  string `json:"path"`
	JobID string `json:"jobId"`
}

// openFolderHandler opens the directory containing a finished output file.
// Only files inside outputDir are accepted, so the endpoint cannot be used to
// open arbitrary paths on the host.
func openFolderHandler(outputDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "только POST", "")
			return
		}

		var req openFolderRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "некорректный JSON", "")
			return
		}

		if req.Path == "" && req.JobID != "" {
			job, ok := getJob(req.JobID)
			if !ok {
				writeJSONError(w, http.StatusNotFound, "задача не найдена", "")
				return
			}
			if job.OutputName == "" || job.Status != "done" {
				writeJSONError(w, http.StatusConflict, "файл ещё не готов", "")
				return
			}
			req.Path = filepath.Join(outputDir, job.OutputName)
		}

		if req.Path == "" {
			writeJSONError(w, http.StatusBadRequest, "path или jobId обязателен", "")
			return
		}

		target, err := resolveOutputFile(outputDir, req.Path)
		if err != nil {
			if errors.Is(err, errOutsideOutput) {
				writeJSONError(w, http.StatusForbidden, err.Error(), "")
				return
			}
			writeJSONError(w, http.StatusNotFound, "файл не найден", "")
			return
		}

		if isDockerEnvironment() {
			writeJSONError(w, http.StatusNotImplemented,
				"открытие папки недоступно в Docker. Скачайте файл или скопируйте путь.", target)
			return
		}

		if err := openInFileManager(target); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error(), target)
			return
		}

		json.NewEncoder(w).Encode(map[string]string{
			"status": "opened",
			"path":   target,
		})
	}
}

func writeJSONError(w http.ResponseWriter, status int, message, path string) {
	w.WriteHeader(status)
	resp := map[string]string{"error": message}
	if path != "" {
		resp["path"] = path
	}
	json.NewEncoder(w).Encode(resp)
}

// resolveOutputFile validates that p points to an existing file inside
// outputDir. Symlinks are resolved so they cannot escape the output folder.
func resolveOutputFile(outputDir, p string) (string, error) {
	base, err := filepath.Abs(outputDir)
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	if !isWithinDir(base, target) {
		return "", errOutsideOutput
	}

	info, err := os.Stat(target)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("это папка, а не файл")
	}

	realBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", err
	}
	realTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", err
	}
	if !isWithinDir(realBase, realTarget) {
		return "", errOutsideOutput
	}
	return realTarget, nil
}

func isWithinDir(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil || filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func isDockerEnvironment() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("DOCKER"))) {
	case "1", "true", "yes":
		return true
	}
	for _, marker := range []string{"/.dockerenv", "/run/.containerenv"} {
		if _, err := os.Stat(marker); err == nil {
			return true
		}
	}
	return false
}

// openInFileManager opens the file's folder and selects the file when the
// platform supports it. Detectable failures are returned so the UI can offer
// a copy-path fallback.
func openInFileManager(filePath string) error {
	abs, err := filepath.Abs(filePath)
	if err != nil {
		return err
	}
	if _, err := os.Stat(abs); err != nil {
		return err
	}

	switch runtime.GOOS {
	case "windows":
		// explorer.exe exits with code 1 even on success, so Start is used and
		// the exit status is intentionally ignored.
		return startDetached("explorer.exe", "/select,", abs)

	case "darwin":
		return startDetached("open", "-R", abs)

	case "linux":
		if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
			return errors.New("графическая сессия не обнаружена (headless Linux)")
		}
		dir := filepath.Dir(abs)
		candidates := [][]string{
			{"nautilus", "--select", abs},
			{"dolphin", "--select", abs},
			{"nemo", abs},
			{"thunar", abs},
		}
		for _, candidate := range candidates {
			if _, err := exec.LookPath(candidate[0]); err != nil {
				continue
			}
			if err := startDetached(candidate[0], candidate[1:]...); err == nil {
				return nil
			}
		}
		if _, err := exec.LookPath("xdg-open"); err != nil {
			return errors.New("файловый менеджер не найден")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "xdg-open", dir).CombinedOutput()
		if err != nil {
			msg := strings.TrimSpace(string(out))
			if msg != "" {
				return fmt.Errorf("xdg-open: %v: %s", err, msg)
			}
			return fmt.Errorf("xdg-open: %w", err)
		}
		return nil

	default:
		return fmt.Errorf("неподдерживаемая ОС: %s", runtime.GOOS)
	}
}

func startDetached(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}
