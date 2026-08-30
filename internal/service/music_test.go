package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"achero_server/internal/biz"
	"achero_server/internal/conf"

	"github.com/gorilla/mux"
)

type fakeRepo struct {
	tracks  []*biz.Track
	index   map[string]*biz.Track
	albums  []*biz.Album
	artists []*biz.Artist
}

func newFakeRepo() *fakeRepo {
	tracks := []*biz.Track{
		{
			ID: "t1", Title: "海阔天空", Artist: "Beyond", Album: "乐与怒",
			AlbumID: "alb1", ArtistID: "art1",
			DurationMs: 324000, FilePath: "t1.mp3", MIMEType: "audio/mpeg",
			CoverMIME: "image/jpeg", CoverData: []byte("jpeg-bytes"),
		},
		{
			ID: "t2", Title: "Song Two", Artist: "Artist", Album: "",
			AlbumID: "alb2", ArtistID: "art2",
			DurationMs: 200000, FilePath: "t2.flac", MIMEType: "audio/flac",
			Lyrics: "[00:00.00]hello\n[00:01.00]world",
		},
	}
	index := map[string]*biz.Track{"t1": tracks[0], "t2": tracks[1]}
	albums := []*biz.Album{
		{ID: "alb1", Name: "乐与怒", Artist: "Beyond", Year: 1993, SongCount: 1, CoverTrackID: "t1"},
		{ID: "alb2", Name: "未知专辑", Artist: "Artist", SongCount: 1},
	}
	artists := []*biz.Artist{
		{ID: "art1", Name: "Beyond", AlbumCount: 1, SongCount: 1, CoverTrackID: "t1"},
		{ID: "art2", Name: "Artist", AlbumCount: 1, SongCount: 1},
	}
	return &fakeRepo{tracks: tracks, index: index, albums: albums, artists: artists}
}

func (f *fakeRepo) ListTracks(_ context.Context, offset, limit int) ([]*biz.Track, int, error) {
	return pageTracks(f.tracks, offset, limit)
}

func (f *fakeRepo) ListAlbums(_ context.Context, offset, limit int) ([]*biz.Album, int, error) {
	total := len(f.albums)
	if offset >= total {
		return []*biz.Album{}, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	out := make([]*biz.Album, end-offset)
	copy(out, f.albums[offset:end])
	return out, total, nil
}

func (f *fakeRepo) ListArtists(_ context.Context, offset, limit int) ([]*biz.Artist, int, error) {
	total := len(f.artists)
	if offset >= total {
		return []*biz.Artist{}, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	out := make([]*biz.Artist, end-offset)
	copy(out, f.artists[offset:end])
	return out, total, nil
}

func (f *fakeRepo) ListSongs(_ context.Context, albumID, artistID string, offset, limit int) ([]*biz.Track, int, error) {
	filtered := make([]*biz.Track, 0)
	for _, t := range f.tracks {
		if albumID != "" && t.AlbumID != albumID {
			continue
		}
		if artistID != "" && t.ArtistID != artistID {
			continue
		}
		filtered = append(filtered, t)
	}
	return pageTracks(filtered, offset, limit)
}

func pageTracks(tracks []*biz.Track, offset, limit int) ([]*biz.Track, int, error) {
	total := len(tracks)
	if offset >= total {
		return []*biz.Track{}, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	out := make([]*biz.Track, end-offset)
	copy(out, tracks[offset:end])
	return out, total, nil
}

func (f *fakeRepo) FindTrack(_ context.Context, id string) (*biz.Track, error) {
	if t := f.index[id]; t != nil {
		return t, nil
	}
	return nil, biz.ErrTrackNotFound
}

func (f *fakeRepo) OpenStream(_ context.Context, id string) (io.ReadSeekCloser, *biz.Track, error) {
	t, err := f.FindTrack(context.Background(), id)
	if err != nil {
		return nil, nil, err
	}
	return readSeekCloser{Reader: bytes.NewReader([]byte("audio-bytes"))}, t, nil
}

type readSeekCloser struct {
	*bytes.Reader
}

func (readSeekCloser) Close() error { return nil }

func testConfig() *conf.Music {
	return &conf.Music{
		StreamSecret: "secret",
		TokenTtl:     3600,
		RpcPath:      "/rpc",
		StreamPath:   "/stream",
		CoverPath:    "/cover",
	}
}

func newTestService(c *conf.Music) *MusicService {
	return NewMusicService(biz.NewMusicUsecase(newFakeRepo()), c)
}

func doRPC(s *MusicService, method, params, token string) *httptest.ResponseRecorder {
	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"` + method + `","params":` + params + `}`)
	req := httptest.NewRequest(http.MethodPost, "/rpc", body)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	s.handleRPC(rec, req)
	return rec
}

func decodeRPC(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return out
}

func TestRPCPing(t *testing.T) {
	s := newTestService(testConfig())
	rec := doRPC(s, "music.ping", "{}", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	out := decodeRPC(t, rec)
	if out["result"].(map[string]any)["ok"] != true {
		t.Fatalf("expected ok=true, got %v", out["result"])
	}
}

func TestRPCList(t *testing.T) {
	s := newTestService(testConfig())
	rec := doRPC(s, "music.list", `{"offset":0,"limit":10}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	out := decodeRPC(t, rec)
	result := out["result"].(map[string]any)
	if result["total"].(float64) != 2 {
		t.Fatalf("total = %v", result["total"])
	}
	tracks := result["tracks"].([]any)
	if len(tracks) != 2 {
		t.Fatalf("tracks len = %d", len(tracks))
	}
	first := tracks[0].(map[string]any)
	if first["id"] != "t1" || first["title"] != "海阔天空" {
		t.Fatalf("unexpected first track: %v", first)
	}
	if url, _ := first["url"].(string); !strings.Contains(url, "/stream/t1?token=") {
		t.Fatalf("expected signed stream url, got %v", first["url"])
	}
	if cover, _ := first["coverUrl"].(string); !strings.Contains(cover, "/cover/t1?token=") {
		t.Fatalf("expected signed cover url, got %v", first["coverUrl"])
	}
	second := tracks[1].(map[string]any)
	if second["lyrics"] == nil || second["coverUrl"] != nil {
		t.Fatalf("unexpected second track fields: %v", second)
	}
}

func TestRPCStreamURL(t *testing.T) {
	s := newTestService(testConfig())
	rec := doRPC(s, "music.streamUrl", `{"id":"t1"}`, "")
	out := decodeRPC(t, rec)
	url, _ := out["result"].(map[string]any)["url"].(string)
	if !strings.Contains(url, "/stream/t1?token=") {
		t.Fatalf("unexpected url: %v", url)
	}
}

func TestRPCListAlbums(t *testing.T) {
	s := newTestService(testConfig())
	rec := doRPC(s, "music.listAlbums", `{"offset":0,"limit":10}`, "")
	out := decodeRPC(t, rec)
	result := out["result"].(map[string]any)
	albums := result["albums"].([]any)
	if len(albums) != 2 {
		t.Fatalf("albums len = %d", len(albums))
	}
	first := albums[0].(map[string]any)
	if first["id"] != "alb1" || first["name"] != "乐与怒" || first["artist"] != "Beyond" || first["year"].(float64) != 1993 {
		t.Fatalf("unexpected first album: %v", first)
	}
	if cover, _ := first["coverUrl"].(string); !strings.Contains(cover, "/cover/t1?token=") {
		t.Fatalf("expected album cover url, got %v", first["coverUrl"])
	}
}

func TestRPCListArtists(t *testing.T) {
	s := newTestService(testConfig())
	rec := doRPC(s, "music.listArtists", `{"offset":0,"limit":10}`, "")
	out := decodeRPC(t, rec)
	result := out["result"].(map[string]any)
	artists := result["artists"].([]any)
	if len(artists) != 2 {
		t.Fatalf("artists len = %d", len(artists))
	}
	first := artists[0].(map[string]any)
	if first["id"] != "art1" || first["name"] != "Beyond" || first["songCount"].(float64) != 1 {
		t.Fatalf("unexpected first artist: %v", first)
	}
}

func TestRPCListSongs(t *testing.T) {
	s := newTestService(testConfig())
	rec := doRPC(s, "music.listSongs", `{"albumId":"alb1"}`, "")
	out := decodeRPC(t, rec)
	result := out["result"].(map[string]any)
	tracks := result["tracks"].([]any)
	if len(tracks) != 1 || tracks[0].(map[string]any)["id"] != "t1" {
		t.Fatalf("unexpected listSongs result: %v", result)
	}
}

func TestRPCListSongsRequiresID(t *testing.T) {
	s := newTestService(testConfig())
	rec := doRPC(s, "music.listSongs", `{}`, "")
	out := decodeRPC(t, rec)
	errObj := out["error"].(map[string]any)
	if int(errObj["code"].(float64)) != -32602 {
		t.Fatalf("code = %v", errObj["code"])
	}
}

func TestRPCUnknownMethod(t *testing.T) {
	s := newTestService(testConfig())
	rec := doRPC(s, "music.nope", "{}", "")
	out := decodeRPC(t, rec)
	errObj := out["error"].(map[string]any)
	if int(errObj["code"].(float64)) != -32601 {
		t.Fatalf("code = %v", errObj["code"])
	}
}

func TestRPCInvalidParams(t *testing.T) {
	s := newTestService(testConfig())
	rec := doRPC(s, "music.streamUrl", `{}`, "")
	out := decodeRPC(t, rec)
	errObj := out["error"].(map[string]any)
	if int(errObj["code"].(float64)) != -32602 {
		t.Fatalf("code = %v", errObj["code"])
	}
}

func TestRPCTrackNotFound(t *testing.T) {
	s := newTestService(testConfig())
	rec := doRPC(s, "music.streamUrl", `{"id":"missing"}`, "")
	out := decodeRPC(t, rec)
	errObj := out["error"].(map[string]any)
	if int(errObj["code"].(float64)) != -32001 {
		t.Fatalf("code = %v", errObj["code"])
	}
}

func TestRPCParseError(t *testing.T) {
	s := newTestService(testConfig())
	req := httptest.NewRequest(http.MethodPost, "/rpc", strings.NewReader("{not json"))
	rec := httptest.NewRecorder()
	s.handleRPC(rec, req)
	out := decodeRPC(t, rec)
	errObj := out["error"].(map[string]any)
	if int(errObj["code"].(float64)) != -32700 {
		t.Fatalf("code = %v", errObj["code"])
	}
}

func TestRPCAuth(t *testing.T) {
	c := testConfig()
	c.Token = "s3cret"
	c.StreamSecret = ""
	s := newTestService(c)

	if rec := doRPC(s, "music.ping", "{}", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", rec.Code)
	}
	if rec := doRPC(s, "music.ping", "{}", "wrong"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with wrong token, got %d", rec.Code)
	}
	if rec := doRPC(s, "music.ping", "{}", "s3cret"); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with token, got %d", rec.Code)
	}
}

func route(h http.HandlerFunc, path string) http.Handler {
	r := mux.NewRouter()
	r.Handle(path, h)
	return r
}

func TestStreamHandler(t *testing.T) {
	s := newTestService(testConfig())
	tok := s.signer.sign("stream", "t1", s.ttl)
	req := httptest.NewRequest(http.MethodGet, "/stream/t1?token="+tok, nil)
	rec := httptest.NewRecorder()
	route(s.handleStream, "/stream/{id}").ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "audio/mpeg" {
		t.Fatalf("content-type = %q", ct)
	}
}

func TestStreamForbidden(t *testing.T) {
	s := newTestService(testConfig())
	req := httptest.NewRequest(http.MethodGet, "/stream/t1?token=bogus", nil)
	rec := httptest.NewRecorder()
	route(s.handleStream, "/stream/{id}").ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestCoverHandler(t *testing.T) {
	s := newTestService(testConfig())
	tok := s.signer.sign("cover", "t1", s.ttl)
	req := httptest.NewRequest(http.MethodGet, "/cover/t1?token="+tok, nil)
	rec := httptest.NewRecorder()
	route(s.handleCover, "/cover/{id}").ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Fatalf("content-type = %q", ct)
	}
	if rec.Body.String() != "jpeg-bytes" {
		t.Fatalf("cover body = %q", rec.Body.String())
	}
}
