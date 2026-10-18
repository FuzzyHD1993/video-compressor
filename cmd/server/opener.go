package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
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
			req.Path = job.OutputPath
			if req.Path == "" {
				req.Path = filepath.Join(outputDir, job.OutputName)
			}
		}

		if req.Path == "" {
			writeJSONError(w, http.StatusBadRequest, "path или jobId обязателен", "")
			return
		}

		var target string
		var err error
		if req.JobID != "" {
			// The job path is server-controlled and may live next to the source
			// file (batch mode), so do not restrict it to output/.
			target, err = existingFile(req.Path)
			if err != nil {
				writeJSONError(w, http.StatusNotFound, "файл не найден", "")
				return
			}
		} else {
			target, err = resolveOutputFile(outputDir, req.Path)
			if err != nil {
				if errors.Is(err, errOutsideOutput) {
					writeJSONError(w, http.StatusForbidden, err.Error(), "")
					return
				}
				writeJSONError(w, http.StatusNotFound, "файл не найден", "")
				return
			}
		}

		if isDockerFunction() && !hasGraphicalSession() {
			writeJSONError(w, http.StatusNotImplemented,
				"открытие папки недоступно без графического окружения. Скачайте файл или скопируйте путь.", target)
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

func existingFile(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("это папка, а не файл")
	}
	return filepath.EvalSymlinks(abs)
}

// isDockerFunction is a variable so tests can simulate a container.
var isDockerFunction = isDockerEnvironment

func writeJSONError(w http.ResponseWriter, status int, message, path string) {
	w.Header().Set("Content-Type", "application/json")
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
	for _, marker := range []string{"/.dockerenv", "/run/.containerenv"} {
		if _, err := os.Stat(marker); err == nil {
			return true
		}
	}
	return false
}

func hasGraphicalSession() bool {
	if runtime.GOOS != "linux" {
		return true
	}
	return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
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
		if !hasGraphicalSession() {
			return errors.New("графическая сессия не обнаружена (headless Linux)")
		}
		dir := filepath.Dir(abs)
		fileURL := (&url.URL{Scheme: "file", Path: abs}).String()

		// Desktop file managers can select the exact file via D-Bus.
		if _, err := exec.LookPath("dbus-send"); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := exec.CommandContext(ctx, "dbus-send",
				"--session",
				"--dest=org.freedesktop.FileManager1",
				"--type=method_call",
				"/org/freedesktop/FileManager1",
				"org.freedesktop.FileManager1.ShowItems",
				"array:string:"+fileURL,
				"string:",
			).Run()
			cancel()
			if err == nil {
				return nil
			}
		}

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
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := exec.LookPath("xdg-open"); err == nil {
			out, err := exec.CommandContext(ctx, "xdg-open", dir).CombinedOutput()
			if err == nil {
				return nil
			}
			msg := strings.TrimSpace(string(out))
			if msg != "" {
				return fmt.Errorf("xdg-open: %v: %s", err, msg)
			}
			return fmt.Errorf("xdg-open: %w", err)
		}
		if _, err := exec.LookPath("gio"); err == nil {
			if out, err := exec.CommandContext(ctx, "gio", "open", dir).CombinedOutput(); err != nil {
				return fmt.Errorf("gio open: %v: %s", err, strings.TrimSpace(string(out)))
			}
			return nil
		}
		return errors.New("файловый менеджер не найден")

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
