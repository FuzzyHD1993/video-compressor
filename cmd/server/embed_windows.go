//go:build windows

package main

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"os"
	"path/filepath"
)

// ffmpeg.exe is embedded into the Windows build so the application can run as
// a single portable executable. The file is intentionally not committed to
// git (it is ~100 MB); put it in cmd/server/ before cross-compiling.
//
//go:embed ffmpeg.exe
var ffmpegBytes []byte

// extractEmbeddedFFmpeg writes the embedded FFmpeg next to the system temp
// directory and returns its path. The file name contains a hash of the binary,
// so upgrading the application does not reuse a stale extraction.
func extractEmbeddedFFmpeg() (string, error) {
	sum := sha256.Sum256(ffmpegBytes)
	name := "video-compressor-ffmpeg-" + hex.EncodeToString(sum[:8]) + ".exe"
	target := filepath.Join(os.TempDir(), name)

	if info, err := os.Stat(target); err == nil && info.Size() == int64(len(ffmpegBytes)) {
		return target, nil
	}

	tmp, err := os.CreateTemp(os.TempDir(), "video-compressor-ffmpeg-*.exe")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(ffmpegBytes); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return "", err
	}

	// On Windows rename does not overwrite an existing file.
	os.Remove(target)
	if err := os.Rename(tmpName, target); err != nil {
		os.Remove(tmpName)
		return "", err
	}
	return target, nil
}
