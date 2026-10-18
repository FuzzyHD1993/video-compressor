package main

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	tempDir   = "./temp"
	outputDir = "./output"
)

type jobRef struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
}

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Только POST", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseMultipartForm(2 << 30); err != nil {
		http.Error(w, "Слишком большой файл", http.StatusBadRequest)
		return
	}

	headers := r.MultipartForm.File["file"]
	if len(headers) == 0 {
		headers = r.MultipartForm.File["video"]
	}
	if len(headers) == 0 {
		http.Error(w, "Файлы не переданы", http.StatusBadRequest)
		return
	}

	profile := r.FormValue("profile")
	if profile == "" {
		profile = r.FormValue("quality")
	}
	profile = normalizeProfile(profile)
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		http.Error(w, "Не удалось создать temp", http.StatusInternalServerError)
		return
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		http.Error(w, "Не удалось создать output", http.StatusInternalServerError)
		return
	}

	refs := make([]jobRef, 0, len(headers))
	for i, header := range headers {
		ext := strings.ToLower(filepath.Ext(header.Filename))
		if !isVideoExtension(ext) {
			continue
		}
		inputPath := filepath.Join(tempDir,
			fmt.Sprintf("input_%d_%d%s", time.Now().UnixNano(), i, ext))
		if err := saveUploadedFile(header, inputPath); err != nil {
			http.Error(w, "Ошибка сохранения: "+err.Error(), http.StatusInternalServerError)
			return
		}
		info, _ := os.Stat(inputPath)
		var size int64
		if info != nil {
			size = info.Size()
		}
		base := strings.TrimSuffix(filepath.Base(header.Filename), ext)
		outName := fmt.Sprintf("%s_%s.mp4", base, profile)
		outputPath := uniqueOutputPath(outputDir, outName)

		job := newJob(header.Filename, inputPath, outputPath, profile, false, size)
		enqueueJob(job)
		refs = append(refs, jobRef{ID: job.ID, Filename: job.Filename})
	}

	if len(refs) == 0 {
		http.Error(w, "Неподдерживаемый формат", http.StatusBadRequest)
		return
	}

	resp := map[string]any{"jobs": refs, "count": len(refs)}
	if len(refs) > 0 {
		resp["jobId"] = refs[0].ID // legacy /upload clients
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func saveUploadedFile(header *multipart.FileHeader, path string) error {
	file, err := header.Open()
	if err != nil {
		return err
	}
	defer file.Close()
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, file); err != nil {
		out.Close()
		os.Remove(path)
		return err
	}
	return out.Close()
}

func jobsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(listJobs())
	case http.MethodPost:
		uploadHandler(w, r)
	default:
		http.Error(w, "Только GET или POST", http.StatusMethodNotAllowed)
	}
}

func jobItemHandler(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/jobs/")
	if id == "" || strings.Contains(id, "/") {
		http.Error(w, "jobId required", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodGet:
		job, ok := getJob(id)
		if !ok {
			http.Error(w, "Job not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(job)
	case http.MethodDelete:
		status, ok := cancelJob(id)
		if !ok {
			http.Error(w, "Job not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if status != "canceled" {
			w.WriteHeader(http.StatusConflict)
		}
		json.NewEncoder(w).Encode(map[string]string{"id": id, "status": status})
	default:
		http.Error(w, "Только GET или DELETE", http.StatusMethodNotAllowed)
	}
}

func batchHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Только POST", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		InputDir  string `json:"input_dir"`
		OutputDir string `json:"output_dir"`
		Profile   string `json:"profile"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "Некорректный JSON", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.InputDir) == "" {
		http.Error(w, "input_dir обязателен", http.StatusBadRequest)
		return
	}
	inputDir, err := filepath.Abs(req.InputDir)
	if err != nil {
		http.Error(w, "Некорректный input_dir", http.StatusBadRequest)
		return
	}
	info, err := os.Stat(inputDir)
	if err != nil || !info.IsDir() {
		http.Error(w, "input_dir не найден или не является папкой", http.StatusBadRequest)
		return
	}
	outDir := strings.TrimSpace(req.OutputDir)
	var outAbs string
	if outDir != "" {
		outAbs, err = filepath.Abs(outDir)
		if err != nil {
			http.Error(w, "Некорректный output_dir", http.StatusBadRequest)
			return
		}
		if err := os.MkdirAll(outAbs, 0o755); err != nil {
			http.Error(w, "Не удалось создать output_dir", http.StatusInternalServerError)
			return
		}
	}

	profile := normalizeProfile(req.Profile)
	files, err := scanVideoFiles(inputDir, outAbs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(files) == 0 {
		http.Error(w, "Видеофайлы не найдены", http.StatusBadRequest)
		return
	}

	refs := make([]jobRef, 0, len(files))
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			continue
		}
		// No output folder given: save next to each source video.
		targetDir := outAbs
		if targetDir == "" {
			targetDir = filepath.Dir(file)
		}
		outputPath := batchOutputPath(inputDir, file, targetDir, profile)
		job := newJob(filepath.Base(file), file, outputPath, profile, true, info.Size())
		enqueueJob(job)
		refs = append(refs, jobRef{ID: job.ID, Filename: job.Filename})
	}

	resultDir := outAbs
	if resultDir == "" {
		resultDir = "рядом с исходными видео"
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"jobs": refs, "count": len(refs), "profile": profile, "output_dir": resultDir,
	})
}

func statusHandler(w http.ResponseWriter, r *http.Request) {
	jobID := r.URL.Query().Get("jobId")
	if jobID == "" {
		http.Error(w, "jobId required", http.StatusBadRequest)
		return
	}
	job, ok := getJob(jobID)
	if !ok {
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(job)
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

func downloadHandler(dir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "Только GET", http.StatusMethodNotAllowed)
			return
		}

		if jobID := r.URL.Query().Get("jobId"); jobID != "" {
			job, ok := getJob(jobID)
			if !ok || job.Status != "done" || job.OutputPath == "" {
				http.Error(w, "Файл не найден", http.StatusNotFound)
				return
			}
			resolved, err := existingFile(job.OutputPath)
			if err != nil {
				http.Error(w, "Файл не найден", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{
				"filename": filepath.Base(resolved),
			}))
			http.ServeFile(w, r, resolved)
			return
		}

		name := r.URL.Query().Get("file")
		if name == "" {
			http.Error(w, "file или jobId required", http.StatusBadRequest)
			return
		}
		if filepath.Base(name) != name || name == "." || name == ".." {
			http.Error(w, "Недопустимое имя файла", http.StatusBadRequest)
			return
		}
		path := filepath.Join(dir, name)
		resolved, err := resolveOutputFile(dir, path)
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
