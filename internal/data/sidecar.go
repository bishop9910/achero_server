package data

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// maxCoverBytes caps the size of an external cover image loaded from a sidecar.
const maxCoverBytes = 5 << 20 // 5 MiB

// trackSidecar is the JSON metadata override that may sit next to an audio file
// (song.mp3 → song.json). Every field is optional: a field present in the JSON
// takes precedence over the audio file's own tags.
type trackSidecar struct {
	Title       string `json:"title"`
	Artist      string `json:"artist"`
	Album       string `json:"album"`
	AlbumArtist string `json:"albumArtist"`
	Year        int    `json:"year"`
	// Cover is a path to an image file, relative to the sidecar's directory. It
	// must stay inside music_dir; absolute paths and escapes are rejected.
	Cover string `json:"cover"`
	// Lyrics is inline LRC text, or a relative path (ending in .lrc/.txt) to a
	// lyrics file. A path follows the same relative/within-root rules as Cover.
	Lyrics string `json:"lyrics"`
}

// readSidecar loads and decodes a JSON sidecar. A missing file is returned as an
// os.IsNotExist error so callers can distinguish "absent" from "malformed".
func readSidecar(path string) (*trackSidecar, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s trackSidecar
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("parse sidecar %s: %w", path, err)
	}
	return &s, nil
}

// resolveSidecarPath validates a relative sidecar path and returns the resolved
// absolute path, rejecting absolute paths and escapes outside root. It is shared
// by cover and lyrics loading.
func resolveSidecarPath(root, dir, rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", fmt.Errorf("empty path")
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("path must be relative: %q", rel)
	}
	full := filepath.Clean(filepath.Join(dir, rel))
	if !withinRoot(root, full) {
		return "", fmt.Errorf("path escapes music_dir: %q", rel)
	}
	return full, nil
}

// looksLikeLyricsPath reports whether a sidecar lyrics value names a lyrics file
// (by extension) rather than holding inline LRC text.
func looksLikeLyricsPath(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.HasSuffix(s, ".lrc") || strings.HasSuffix(s, ".txt")
}

// loadCover reads a cover image referenced by a sidecar. rel must be a relative
// path (relative to dir) that resolves inside root; anything else is rejected so
// a sidecar cannot reach arbitrary files on the host.
func loadCover(root, dir, rel string) ([]byte, string, error) {
	full, err := resolveSidecarPath(root, dir, rel)
	if err != nil {
		return nil, "", err
	}

	st, err := os.Stat(full)
	if err != nil {
		return nil, "", err
	}
	if st.Size() > maxCoverBytes {
		return nil, "", fmt.Errorf("cover too large: %q", rel)
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return nil, "", err
	}
	return b, http.DetectContentType(b), nil
}

// withinRoot reports whether path is at or below root (no ".." that escapes).
func withinRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
