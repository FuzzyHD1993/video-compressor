//go:build !windows

package main

// extractEmbeddedFFmpeg is a no-op outside Windows: Linux builds use the
// system FFmpeg (or the one bundled in the Docker image).
func extractEmbeddedFFmpeg() (string, error) {
	return "", nil
}
