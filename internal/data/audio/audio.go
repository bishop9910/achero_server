// Package audio extracts metadata and duration from audio files. It is
// deliberately best-effort: any field that cannot be parsed is left at its zero
// value rather than failing the whole file.
package audio

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/dhowden/tag"
)

// Info is the extracted metadata for one audio file.
type Info struct {
	Title       string
	Artist      string
	Album       string
	AlbumArtist string
	Year        int
	DurationMs  int64
	MIMEType    string
	CoverMIME   string
	Cover       []byte
}

var mimeByExt = map[string]string{
	".mp3":  "audio/mpeg",
	".flac": "audio/flac",
	".m4a":  "audio/mp4",
	".m4b":  "audio/mp4",
	".ogg":  "audio/ogg",
	".opus": "audio/ogg",
	".wav":  "audio/wav",
	".aac":  "audio/aac",
	".wma":  "audio/x-ms-wma",
}

// Read extracts metadata from the audio file at path.
func Read(path string) (Info, error) {
	ext := strings.ToLower(filepath.Ext(path))
	info := Info{MIMEType: mimeByExt[ext]}

	f, err := os.Open(path)
	if err != nil {
		return info, err
	}
	defer f.Close()

	// dhowden/tag reads ID3v2, MP4, FLAC and Vorbis comments. WAV and other
	// untagged formats simply yield no tags and fall back to the filename.
	if m, err := tag.ReadFrom(f); err == nil {
		info.Title = m.Title()
		info.Artist = m.Artist()
		info.Album = m.Album()
		info.AlbumArtist = m.AlbumArtist()
		info.Year = m.Year()
		if p := m.Picture(); p != nil {
			info.Cover = p.Data
			info.CoverMIME = p.MIMEType
		}
	}

	if info.Title == "" {
		base := filepath.Base(path)
		info.Title = strings.TrimSuffix(base, filepath.Ext(base))
	}

	info.DurationMs, _ = duration(path, ext)
	return info, nil
}
