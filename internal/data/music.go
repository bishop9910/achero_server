package data

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"achero_server/internal/biz"
	"achero_server/internal/conf"
	"achero_server/internal/data/audio"

	"github.com/fsnotify/fsnotify"
	"github.com/go-kratos/kratos/v3/log"
	"golang.org/x/text/encoding/simplifiedchinese"
)

// defaultExtensions are indexed when the config does not override them.
var defaultExtensions = []string{"mp3", "flac", "m4a", "m4b", "ogg", "opus", "wav"}

// watchDebounce is how long to wait for a burst of filesystem events to settle
// before rescanning. A package var so tests can shorten it.
var watchDebounce = 2 * time.Second

// musicRepo is the filesystem-backed MusicRepo implementation. It scans a
// directory into an immutable in-memory index and serves the files directly.
type musicRepo struct {
	root    string
	exts    map[string]bool
	mu      sync.RWMutex
	tracks  []*biz.Track
	index   map[string]*biz.Track
	albums  []*biz.Album
	artists []*biz.Artist
	cancel  context.CancelFunc
}

// NewMusicRepo scans the configured music directory and returns a MusicRepo.
// The returned cleanup function stops the background rescan loop, when enabled.
func NewMusicRepo(c *conf.Music) (biz.MusicRepo, func(), error) {
	exts := make(map[string]bool)
	if len(c.GetExtensions()) == 0 {
		for _, e := range defaultExtensions {
			exts[e] = true
		}
	} else {
		for _, e := range c.GetExtensions() {
			exts[strings.TrimPrefix(strings.ToLower(strings.TrimSpace(e)), ".")] = true
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	r := &musicRepo{
		root:   c.GetMusicDir(),
		exts:   exts,
		cancel: cancel,
	}
	log.Info("music library scanning", "dir", r.root)
	if err := r.scan(); err != nil {
		log.Warn("music library scan issue", "err", err)
	}

	if d := c.GetRescanInterval(); d != nil && d.AsDuration() > 0 {
		go r.rescanLoop(ctx, d.AsDuration())
	}

	var watcher *fsnotify.Watcher
	if c.GetWatch() && r.root != "" {
		w, err := fsnotify.NewWatcher()
		if err != nil {
			log.Warn("music watch disabled", "err", err)
		} else if err := r.addDirWatches(w); err != nil {
			log.Warn("music watch setup failed", "err", err)
			_ = w.Close()
		} else {
			watcher = w
			go r.watchLoop(ctx, w)
			log.Info("music library watching for changes", "dir", r.root)
		}
	}

	return r, func() {
		cancel()
		if watcher != nil {
			_ = watcher.Close()
		}
	}, nil
}

// scan walks the music directory and atomically swaps in a fresh index.
func (r *musicRepo) scan() error {
	if r.root == "" {
		log.Warn("music_dir is empty; serving an empty library")
		r.swap(nil)
		return nil
	}
	st, err := os.Stat(r.root)
	if err != nil {
		r.swap(nil)
		return err
	}
	if !st.IsDir() {
		r.swap(nil)
		return fmt.Errorf("music_dir is not a directory: %s", r.root)
	}

	var tracks []*biz.Track
	err = filepath.WalkDir(r.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
		if !r.exts[ext] {
			return nil
		}
		t, err := r.indexFile(path)
		if err != nil {
			log.Warn("skipping audio file", "path", path, "err", err)
			return nil
		}
		tracks = append(tracks, t)
		return nil
	})
	r.swap(tracks)
	if err != nil {
		return err
	}
	return nil
}

func (r *musicRepo) indexFile(path string) (*biz.Track, error) {
	rel, err := filepath.Rel(r.root, path)
	if err != nil {
		rel = filepath.Base(path)
	}
	rel = filepath.ToSlash(rel)

	info, err := audio.Read(path)
	if err != nil {
		return nil, err
	}

	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	// Start from the audio file's own tags, then let a sidecar JSON
	// (song.mp3 → song.json) override any field it specifies.
	title := info.Title
	artist := info.Artist
	album := info.Album
	albumArtist := info.AlbumArtist
	year := info.Year
	coverMIME := info.CoverMIME
	coverData := info.Cover
	lyrics := readLyrics(strings.TrimSuffix(path, filepath.Ext(path)) + ".lrc")

	if sc, err := readSidecar(strings.TrimSuffix(path, filepath.Ext(path)) + ".json"); err == nil {
		if sc.Title != "" {
			title = sc.Title
		}
		if sc.Artist != "" {
			artist = sc.Artist
		}
		if sc.Album != "" {
			album = sc.Album
		}
		if sc.AlbumArtist != "" {
			albumArtist = sc.AlbumArtist
		}
		if sc.Year != 0 {
			year = sc.Year
		}
		if sc.Lyrics != "" {
			if looksLikeLyricsPath(sc.Lyrics) {
				if full, err := resolveSidecarPath(r.root, filepath.Dir(path), sc.Lyrics); err != nil {
					log.Warn("sidecar lyrics path invalid", "path", path, "err", err)
				} else {
					lyrics = readLyrics(full)
				}
			} else {
				lyrics = sc.Lyrics
			}
		}
		if sc.Cover != "" {
			if d, m, err := loadCover(r.root, filepath.Dir(path), sc.Cover); err != nil {
				log.Warn("sidecar cover load failed", "path", path, "err", err)
			} else {
				coverData = d
				coverMIME = m
			}
		}
	} else if !os.IsNotExist(err) {
		log.Warn("sidecar parse failed", "path", path, "err", err)
	}

	artistName := displayName(artist, "未知艺术家")
	albumName := displayName(album, "未知专辑")
	albumArtistName := displayName(albumArtist, artistName)

	return &biz.Track{
		ID:          hashID(rel),
		Title:       title,
		Artist:      artist,
		Album:       album,
		AlbumID:     hashID(albumArtistName + "\x00" + albumName),
		ArtistID:    hashID(artistName),
		AlbumArtist: albumArtistName,
		Year:        year,
		DurationMs:  info.DurationMs,
		FilePath:    path,
		MIMEType:    info.MIMEType,
		FileSize:    st.Size(),
		ModTime:     st.ModTime(),
		Lyrics:      lyrics,
		CoverMIME:   coverMIME,
		CoverData:   coverData,
	}, nil
}

// swap replaces the index atomically under the write lock. Track values are
// immutable after creation, so readers can safely hold a pointer after the lock
// is released. Album and artist views are derived from the tracks here.
func (r *musicRepo) swap(tracks []*biz.Track) {
	sort.Slice(tracks, func(i, j int) bool { return tracks[i].ID < tracks[j].ID })
	index := make(map[string]*biz.Track, len(tracks))
	for _, t := range tracks {
		index[t.ID] = t
	}
	albums, artists := buildAlbumsAndArtists(tracks)

	r.mu.Lock()
	r.tracks = tracks
	r.index = index
	r.albums = albums
	r.artists = artists
	r.mu.Unlock()
	log.Info("music library indexed", "count", len(tracks), "albums", len(albums), "artists", len(artists))
}

// buildAlbumsAndArtists aggregates the track list into album and artist views.
// An album is keyed by (album artist + name) so same-name albums by different
// artists stay distinct; compilations without an AlbumArtist tag are keyed by
// their track artist and therefore appear as separate entries.
func buildAlbumsAndArtists(tracks []*biz.Track) ([]*biz.Album, []*biz.Artist) {
	type albumAgg struct {
		album biz.Album
	}
	type artistAgg struct {
		artist   biz.Artist
		albumIDs map[string]bool
	}

	albumMap := make(map[string]*albumAgg)
	artistMap := make(map[string]*artistAgg)

	for _, t := range tracks {
		artistName := displayName(t.Artist, "未知艺术家")
		albumName := displayName(t.Album, "未知专辑")
		albumArtist := t.AlbumArtist
		if albumArtist == "" {
			albumArtist = artistName
		}

		a := albumMap[t.AlbumID]
		if a == nil {
			a = &albumAgg{album: biz.Album{ID: t.AlbumID, Name: albumName, Artist: albumArtist, Year: t.Year}}
			albumMap[t.AlbumID] = a
		}
		a.album.SongCount++
		if a.album.CoverTrackID == "" && t.CoverMIME != "" {
			a.album.CoverTrackID = t.ID
		}
		if a.album.Year == 0 && t.Year != 0 {
			a.album.Year = t.Year
		}

		ar := artistMap[t.ArtistID]
		if ar == nil {
			ar = &artistAgg{artist: biz.Artist{ID: t.ArtistID, Name: artistName}, albumIDs: make(map[string]bool)}
			artistMap[t.ArtistID] = ar
		}
		ar.artist.SongCount++
		if !ar.albumIDs[t.AlbumID] {
			ar.albumIDs[t.AlbumID] = true
			ar.artist.AlbumCount++
		}
		if ar.artist.CoverTrackID == "" && t.CoverMIME != "" {
			ar.artist.CoverTrackID = t.ID
		}
	}

	albums := make([]*biz.Album, 0, len(albumMap))
	for _, a := range albumMap {
		albums = append(albums, &a.album)
	}
	sort.Slice(albums, func(i, j int) bool { return albums[i].Name < albums[j].Name })

	artists := make([]*biz.Artist, 0, len(artistMap))
	for _, a := range artistMap {
		artists = append(artists, &a.artist)
	}
	sort.Slice(artists, func(i, j int) bool { return artists[i].Name < artists[j].Name })

	return albums, artists
}

// displayName normalizes a possibly-empty metadata string to a displayable one.
func displayName(s, fallback string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return fallback
	}
	return s
}

func (r *musicRepo) rescanLoop(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := r.scan(); err != nil {
				log.Warn("music library rescan issue", "err", err)
			}
		}
	}
}

// addDirWatches registers a filesystem watch on the music root and every
// subdirectory. Watching directories (rather than individual files) is enough:
// entries appearing, disappearing or being renamed are all reported as events
// on the containing directory. New directories are picked up by re-syncing after
// each rescan.
func (r *musicRepo) addDirWatches(w *fsnotify.Watcher) error {
	return filepath.WalkDir(r.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries, keep walking the rest
		}
		if d.IsDir() {
			if err := w.Add(path); err != nil {
				log.Warn("music watch add failed", "path", path, "err", err)
			}
		}
		return nil
	})
}

// watchLoop drains fsnotify events and debounces them into a full rescan. The
// debounce collapses the burst of Create/Write events a single file copy
// produces into one rescan after the dust settles.
func (r *musicRepo) watchLoop(ctx context.Context, w *fsnotify.Watcher) {
	var timer *time.Timer
	var timerC <-chan time.Time

	trigger := func() {
		if timer == nil {
			timer = time.NewTimer(watchDebounce)
			timerC = timer.C
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(watchDebounce)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			log.Warn("music watch error", "err", err)
		case evt, ok := <-w.Events:
			if !ok {
				return
			}
			if evt.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) == 0 {
				continue
			}
			trigger()
		case <-timerC:
			timer = nil
			timerC = nil
			if err := r.scan(); err != nil {
				log.Warn("music library rescan issue", "err", err)
			}
			// Cover directories created or removed since the last sync.
			_ = r.addDirWatches(w)
		}
	}
}

func (r *musicRepo) ListTracks(_ context.Context, offset, limit int) ([]*biz.Track, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	total := len(r.tracks)
	if offset >= total {
		return []*biz.Track{}, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	out := make([]*biz.Track, end-offset)
	copy(out, r.tracks[offset:end])
	return out, total, nil
}

func (r *musicRepo) ListAlbums(_ context.Context, offset, limit int) ([]*biz.Album, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	total := len(r.albums)
	if offset >= total {
		return []*biz.Album{}, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	out := make([]*biz.Album, end-offset)
	copy(out, r.albums[offset:end])
	return out, total, nil
}

func (r *musicRepo) ListArtists(_ context.Context, offset, limit int) ([]*biz.Artist, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	total := len(r.artists)
	if offset >= total {
		return []*biz.Artist{}, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	out := make([]*biz.Artist, end-offset)
	copy(out, r.artists[offset:end])
	return out, total, nil
}

func (r *musicRepo) ListSongs(_ context.Context, albumID, artistID string, offset, limit int) ([]*biz.Track, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	filtered := make([]*biz.Track, 0)
	for _, t := range r.tracks {
		if albumID != "" && t.AlbumID != albumID {
			continue
		}
		if artistID != "" && t.ArtistID != artistID {
			continue
		}
		filtered = append(filtered, t)
	}
	total := len(filtered)
	if offset >= total {
		return []*biz.Track{}, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	out := make([]*biz.Track, end-offset)
	copy(out, filtered[offset:end])
	return out, total, nil
}

func (r *musicRepo) FindTrack(_ context.Context, id string) (*biz.Track, error) {
	r.mu.RLock()
	t := r.index[id]
	r.mu.RUnlock()
	if t == nil {
		return nil, biz.ErrTrackNotFound
	}
	return t, nil
}

func (r *musicRepo) OpenStream(ctx context.Context, id string) (io.ReadSeekCloser, *biz.Track, error) {
	t, err := r.FindTrack(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.Open(t.FilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, biz.ErrTrackNotFound
		}
		return nil, nil, err
	}
	return f, t, nil
}

// hashID derives a stable, URL-safe id from an arbitrary string (a relative
// file path, an artist name, or an album key). Hashing keeps ids opaque and
// immune to path traversal: ids are always resolved through the in-memory index.
func hashID(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:24]
}

// readLyrics loads a sidecar .lrc file, capped to avoid ballooning memory.
func readLyrics(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil {
		return ""
	}
	return decodeLyrics(b)
}

// decodeLyrics normalizes lyric text to UTF-8. Chinese .lrc files are commonly
// encoded in GBK/GB2312 (or carry a UTF-8 BOM) rather than UTF-8; serving those
// bytes as-is produces mojibake in the JSON output. We keep valid UTF-8 as-is
// and fall back to GB18030 (a superset of GBK/GB2312) otherwise.
func decodeLyrics(b []byte) string {
	b = bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF}) // UTF-8 BOM
	if utf8.Valid(b) {
		return string(b)
	}
	if out, err := simplifiedchinese.GB18030.NewDecoder().Bytes(b); err == nil {
		return string(out)
	}
	return string(b)
}
