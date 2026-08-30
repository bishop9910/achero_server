package data

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIndexFileSidecarOverride(t *testing.T) {
	dir, err := os.MkdirTemp(".", "sidecar-test-")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	defer os.RemoveAll(dir)

	if err := os.WriteFile(filepath.Join(dir, "song.mp3"), []byte("fake-audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	coverBytes := []byte("\xff\xd8\xff\xe0fake-jpeg")
	if err := os.WriteFile(filepath.Join(dir, "cover.jpg"), coverBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	sidecar := `{"title":"自定义标题","artist":"自定义艺术家","album":"自定义专辑","year":2024,"cover":"cover.jpg"}`
	if err := os.WriteFile(filepath.Join(dir, "song.json"), []byte(sidecar), 0o644); err != nil {
		t.Fatal(err)
	}

	r := &musicRepo{root: dir, exts: map[string]bool{"mp3": true}}
	track, err := r.indexFile(filepath.Join(dir, "song.mp3"))
	if err != nil {
		t.Fatalf("indexFile: %v", err)
	}

	if track.Title != "自定义标题" || track.Artist != "自定义艺术家" || track.Album != "自定义专辑" || track.Year != 2024 {
		t.Fatalf("sidecar override failed: %+v", track)
	}
	if string(track.CoverData) != string(coverBytes) {
		t.Fatalf("cover data mismatch: %q", track.CoverData)
	}
	if track.CoverMIME != "image/jpeg" {
		t.Fatalf("cover mime = %q", track.CoverMIME)
	}
}

func TestLoadCoverRejectsUnsafePath(t *testing.T) {
	dir, err := os.MkdirTemp(".", "cover-test-")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	defer os.RemoveAll(dir)

	if _, _, err := loadCover(dir, dir, "/etc/passwd"); err == nil {
		t.Fatal("absolute path should be rejected")
	}
	if _, _, err := loadCover(dir, dir, "../outside.jpg"); err == nil {
		t.Fatal("escaping path should be rejected")
	}

	if err := os.WriteFile(filepath.Join(dir, "cover.jpg"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadCover(dir, dir, "cover.jpg"); err != nil {
		t.Fatalf("relative path should load: %v", err)
	}
}

func TestReadSidecarErrors(t *testing.T) {
	dir, err := os.MkdirTemp(".", "sidecar-err-")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	defer os.RemoveAll(dir)

	if _, err := readSidecar(filepath.Join(dir, "missing.json")); !os.IsNotExist(err) {
		t.Fatalf("expected IsNotExist, got %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readSidecar(filepath.Join(dir, "bad.json")); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestSidecarLyricsFile(t *testing.T) {
	dir, err := os.MkdirTemp(".", "lyrics-test-")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	defer os.RemoveAll(dir)

	if err := os.WriteFile(filepath.Join(dir, "song.mp3"), []byte("fake-audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lyrics.lrc"), []byte("[00:00.00]hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "song.json"), []byte(`{"lyrics":"lyrics.lrc"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	r := &musicRepo{root: dir, exts: map[string]bool{"mp3": true}}
	track, err := r.indexFile(filepath.Join(dir, "song.mp3"))
	if err != nil {
		t.Fatalf("indexFile: %v", err)
	}
	if track.Lyrics != "[00:00.00]hello" {
		t.Fatalf("lyrics file not loaded: %q", track.Lyrics)
	}
}

func TestSidecarLyricsInline(t *testing.T) {
	dir, err := os.MkdirTemp(".", "lyrics-inline-")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	defer os.RemoveAll(dir)

	if err := os.WriteFile(filepath.Join(dir, "song.mp3"), []byte("fake-audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "song.json"), []byte(`{"lyrics":"[00:01.00]inline"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	r := &musicRepo{root: dir, exts: map[string]bool{"mp3": true}}
	track, err := r.indexFile(filepath.Join(dir, "song.mp3"))
	if err != nil {
		t.Fatalf("indexFile: %v", err)
	}
	if track.Lyrics != "[00:01.00]inline" {
		t.Fatalf("inline lyrics mismatch: %q", track.Lyrics)
	}
}
