package data

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"acheron_server/internal/biz"
	"acheron_server/internal/conf"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestDecodeLyricsUTF8Unchanged(t *testing.T) {
	in := "[00:00.00]作词：ROVIN /ISOMN\n[00:01.00]作曲：Jam Strings"
	if got := decodeLyrics([]byte(in)); got != in {
		t.Fatalf("utf8 input changed: %q", got)
	}
}

func TestDecodeLyricsUTF8BOMStripped(t *testing.T) {
	in := "\xEF\xBB\xBF[00:00.00]hello"
	if got := decodeLyrics([]byte(in)); got != "[00:00.00]hello" {
		t.Fatalf("BOM not stripped: %q", got)
	}
}

func TestDecodeLyricsGBKToUTF8(t *testing.T) {
	src := "[00:00.00]作词：ROVIN /ISOMN"
	gbk, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(src))
	if err != nil {
		t.Fatalf("encode gbk: %v", err)
	}
	got := decodeLyrics(gbk)
	if got != src {
		t.Fatalf("gbk decode mismatch:\n got: %q\nwant: %q", got, src)
	}
}

func TestWatchHotReload(t *testing.T) {
	old := watchDebounce
	watchDebounce = 100 * time.Millisecond
	defer func() { watchDebounce = old }()

	dir, err := os.MkdirTemp(".", "watch-test-")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	defer os.RemoveAll(dir)

	repo, cleanup, err := NewMusicRepo(&conf.Music{MusicDir: dir, Watch: true})
	if err != nil {
		t.Fatalf("NewMusicRepo: %v", err)
	}
	defer cleanup()

	if _, total, err := repo.ListTracks(context.Background(), 0, 10); err != nil || total != 0 {
		t.Fatalf("expected empty library, total=%d err=%v", total, err)
	}

	// Drop a new audio file into the watched directory. The bytes do not need
	// to be a valid audio file: audio.Read tolerates untagged files and falls
	// back to the filename for the title.
	if err := os.WriteFile(filepath.Join(dir, "song.mp3"), []byte("fake-audio"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		_, total, err := repo.ListTracks(context.Background(), 0, 10)
		if err == nil && total == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("hot reload did not pick up new file within timeout (total=%d err=%v)", total, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestBuildAlbumsAndArtists(t *testing.T) {
	tracks := []*biz.Track{
		{ID: "t1", Artist: "Beyond", Album: "乐与怒", AlbumID: "a1", ArtistID: "ar1", AlbumArtist: "Beyond", Year: 1993, CoverMIME: "image/jpeg"},
		{ID: "t2", Artist: "Beyond", Album: "乐与怒", AlbumID: "a1", ArtistID: "ar1", AlbumArtist: "Beyond", Year: 1993},
		{ID: "t3", Artist: "Beyond", Album: "海阔天空", AlbumID: "a2", ArtistID: "ar1", AlbumArtist: "Beyond"},
	}
	albums, artists := buildAlbumsAndArtists(tracks)

	if len(albums) != 2 {
		t.Fatalf("expected 2 albums, got %d", len(albums))
	}
	if len(artists) != 1 {
		t.Fatalf("expected 1 artist, got %d", len(artists))
	}

	ar := artists[0]
	if ar.Name != "Beyond" || ar.AlbumCount != 2 || ar.SongCount != 3 || ar.CoverTrackID != "t1" {
		t.Fatalf("unexpected artist: %+v", ar)
	}

	var a1 *biz.Album
	for _, a := range albums {
		if a.ID == "a1" {
			a1 = a
		}
	}
	if a1 == nil || a1.SongCount != 2 || a1.CoverTrackID != "t1" || a1.Year != 1993 || a1.Artist != "Beyond" {
		t.Fatalf("unexpected album a1: %+v", a1)
	}
}

func TestBuildAlbumsAndArtistsUnknownFallback(t *testing.T) {
	tracks := []*biz.Track{
		{ID: "t1", AlbumID: "u1", ArtistID: "u2"},
	}
	albums, artists := buildAlbumsAndArtists(tracks)
	if len(albums) != 1 || albums[0].Name != "未知专辑" {
		t.Fatalf("album fallback failed: %+v", albums)
	}
	if len(artists) != 1 || artists[0].Name != "未知艺术家" {
		t.Fatalf("artist fallback failed: %+v", artists)
	}
}
