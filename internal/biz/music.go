package biz

import (
	"context"
	"errors"
	"io"
	"time"
)

// Sentinel errors for the music domain. The transport here is JSON-RPC rather
// than protobuf, so these are plain errors instead of the *kratos/errors.Status
// values used by proto-backed resources; the service layer maps them onto
// JSON-RPC error codes.
var (
	// ErrTrackNotFound is returned when a track id does not resolve.
	ErrTrackNotFound = errors.New("track not found")
	// ErrInvalidParams is returned when a request parameter is missing or malformed.
	ErrInvalidParams = errors.New("invalid parameters")
)

// Track is the domain model for a single music track.
type Track struct {
	ID         string
	Title      string
	Artist     string
	Album      string
	// AlbumID and ArtistID are stable derived identifiers (hash of album/artist
	// name) used to answer listSongs without leaking the raw strings.
	AlbumID    string
	ArtistID   string
	// AlbumArtist and Year feed album aggregation; they are not serialized in
	// the track JSON response.
	AlbumArtist string
	Year        int
	DurationMs int64
	// FilePath is the absolute path to the audio file on the server.
	FilePath string
	MIMEType string
	FileSize int64
	ModTime  time.Time
	// Lyrics is inline LRC text when a sidecar .lrc file exists.
	Lyrics string
	// CoverMIME and CoverData hold embedded cover art, when present.
	CoverMIME string
	CoverData []byte
}

// Album is the domain model for a browsable album.
type Album struct {
	ID      string
	Name    string
	Artist  string
	Year    int
	SongCount int
	// CoverTrackID is the id of a track whose embedded cover represents this
	// album; empty when no track in the album has a cover.
	CoverTrackID string
}

// Artist is the domain model for a browsable artist.
type Artist struct {
	ID         string
	Name       string
	AlbumCount int
	SongCount  int
	// CoverTrackID is the id of a track whose embedded cover represents this
	// artist; empty when the artist has no covered track.
	CoverTrackID string
}

// MusicRepo is the music library access seam. The data layer implements it
// against the filesystem.
type MusicRepo interface {
	// ListTracks returns a page of tracks plus the total track count.
	ListTracks(ctx context.Context, offset, limit int) ([]*Track, int, error)
	// ListAlbums returns a page of albums plus the total album count.
	ListAlbums(ctx context.Context, offset, limit int) ([]*Album, int, error)
	// ListArtists returns a page of artists plus the total artist count.
	ListArtists(ctx context.Context, offset, limit int) ([]*Artist, int, error)
	// ListSongs returns tracks matching the given album or artist id.
	ListSongs(ctx context.Context, albumID, artistID string, offset, limit int) ([]*Track, int, error)
	// FindTrack resolves a track by its id.
	FindTrack(ctx context.Context, id string) (*Track, error)
	// OpenStream opens the audio file for a track for streaming.
	OpenStream(ctx context.Context, id string) (io.ReadSeekCloser, *Track, error)
}

// MusicUsecase is the music usecase.
type MusicUsecase struct {
	repo MusicRepo
}

// NewMusicUsecase creates a MusicUsecase.
func NewMusicUsecase(repo MusicRepo) *MusicUsecase {
	return &MusicUsecase{repo: repo}
}

// ListTracks lists tracks with sane defaults applied.
func (uc *MusicUsecase) ListTracks(ctx context.Context, offset, limit int) ([]*Track, int, error) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 200
	}
	return uc.repo.ListTracks(ctx, offset, limit)
}

// ListAlbums lists albums with sane defaults applied.
func (uc *MusicUsecase) ListAlbums(ctx context.Context, offset, limit int) ([]*Album, int, error) {
	return uc.repo.ListAlbums(ctx, offset, limit)
}

// ListArtists lists artists with sane defaults applied.
func (uc *MusicUsecase) ListArtists(ctx context.Context, offset, limit int) ([]*Artist, int, error) {
	return uc.repo.ListArtists(ctx, offset, limit)
}

// ListSongs lists songs by album or artist id.
func (uc *MusicUsecase) ListSongs(ctx context.Context, albumID, artistID string, offset, limit int) ([]*Track, int, error) {
	if albumID == "" && artistID == "" {
		return nil, 0, ErrInvalidParams
	}
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 200
	}
	return uc.repo.ListSongs(ctx, albumID, artistID, offset, limit)
}

// GetTrack resolves a single track.
func (uc *MusicUsecase) GetTrack(ctx context.Context, id string) (*Track, error) {
	if id == "" {
		return nil, ErrInvalidParams
	}
	return uc.repo.FindTrack(ctx, id)
}

// OpenStream opens a track's audio file.
func (uc *MusicUsecase) OpenStream(ctx context.Context, id string) (io.ReadSeekCloser, *Track, error) {
	if id == "" {
		return nil, nil, ErrInvalidParams
	}
	return uc.repo.OpenStream(ctx, id)
}
