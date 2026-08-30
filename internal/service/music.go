package service

import (
	"crypto/hmac"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"acheron_server/internal/biz"
	"acheron_server/internal/conf"

	"github.com/go-kratos/kratos/v3/log"
	khttp "github.com/go-kratos/kratos/v3/transport/http"
	"github.com/gorilla/mux"
)

const (
	defaultRPCPath    = "/rpc"
	defaultStreamPath = "/stream"
	defaultCoverPath  = "/cover"
	defaultTokenTTL   = int64(3600)
	defaultListLimit  = 200
	maxRPCBodyBytes   = 1 << 20 // 1 MiB
)

// JSON-RPC error codes. -32700..-32602 are standard; -32000..-32001 are
// application-defined.
const (
	rpcParseError     = -32700
	rpcInvalidRequest = -32600
	rpcMethodNotFound = -32601
	rpcInvalidParams  = -32602
	rpcInternalError  = -32000
	rpcTrackNotFound  = -32001
)

// MusicService adapts the Achero JSON-RPC music protocol onto the MusicUsecase.
type MusicService struct {
	uc         *biz.MusicUsecase
	rpcPath    string
	streamPath string
	coverPath  string
	baseURL    string
	token      string
	secret     string
	ttl        int64
	signer     *signer
}

// NewMusicService creates a MusicService from the usecase and music config.
func NewMusicService(uc *biz.MusicUsecase, c *conf.Music) *MusicService {
	s := &MusicService{
		uc:      uc,
		baseURL: strings.TrimRight(c.GetBaseUrl(), "/"),
		token:   strings.TrimSpace(c.GetToken()),
		ttl:     c.GetTokenTtl(),
	}
	if s.ttl <= 0 {
		s.ttl = defaultTokenTTL
	}
	s.secret = c.GetStreamSecret()
	if s.secret == "" {
		s.secret = s.token
	}
	s.signer = newSigner(s.secret)
	s.rpcPath = withSlash(c.GetRpcPath(), defaultRPCPath)
	s.streamPath = withSlash(c.GetStreamPath(), defaultStreamPath)
	s.coverPath = withSlash(c.GetCoverPath(), defaultCoverPath)
	return s
}

// RegisterHTTP wires the music routes onto a Kratos HTTP server.
func (s *MusicService) RegisterHTTP(srv *khttp.Server) {
	srv.Handle(s.rpcPath, http.HandlerFunc(s.handleRPC))
	srv.Handle(s.streamPath+"/{id}", http.HandlerFunc(s.handleStream))
	srv.Handle(s.coverPath+"/{id}", http.HandlerFunc(s.handleCover))
}

// --- JSON-RPC ---

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcErrorObj    `json:"error,omitempty"`
}

type rpcErrorObj struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *MusicService) handleRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !s.authorizeRPC(r) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="music"`)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	body, err := readAllLimit(r, maxRPCBodyBytes)
	if err != nil {
		s.writeRPCError(w, nil, rpcParseError, "cannot read body")
		return
	}
	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		s.writeRPCError(w, nil, rpcParseError, "parse error")
		return
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		s.writeRPCError(w, req.ID, rpcInvalidRequest, "invalid request")
		return
	}

	switch req.Method {
	case "music.ping":
		s.writeRPCResult(w, req.ID, map[string]bool{"ok": true})
	case "music.list":
		s.rpcList(w, r, &req)
	case "music.listAlbums":
		s.rpcListAlbums(w, r, &req)
	case "music.listArtists":
		s.rpcListArtists(w, r, &req)
	case "music.listSongs":
		s.rpcListSongs(w, r, &req)
	case "music.streamUrl":
		s.rpcStreamURL(w, r, &req)
	default:
		s.writeRPCError(w, req.ID, rpcMethodNotFound, "unknown method")
	}
}

func (s *MusicService) rpcList(w http.ResponseWriter, r *http.Request, req *rpcRequest) {
	var p struct {
		Offset int `json:"offset"`
		Limit  int `json:"limit"`
	}
	if len(req.Params) > 0 && string(req.Params) != "null" {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			s.writeRPCError(w, req.ID, rpcInvalidParams, "invalid params")
			return
		}
	}
	if p.Offset < 0 {
		p.Offset = 0
	}
	if p.Limit <= 0 {
		p.Limit = defaultListLimit
	}

	tracks, total, err := s.uc.ListTracks(r.Context(), p.Offset, p.Limit)
	if err != nil {
		log.Error("music list failed", "err", err)
		s.writeRPCError(w, req.ID, rpcInternalError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(tracks))
	for _, t := range tracks {
		out = append(out, s.trackJSON(r, t))
	}
	s.writeRPCResult(w, req.ID, map[string]any{"tracks": out, "total": total})
}

func (s *MusicService) rpcListAlbums(w http.ResponseWriter, r *http.Request, req *rpcRequest) {
	var p struct {
		Offset int `json:"offset"`
		Limit  int `json:"limit"`
	}
	if len(req.Params) > 0 && string(req.Params) != "null" {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			s.writeRPCError(w, req.ID, rpcInvalidParams, "invalid params")
			return
		}
	}
	if p.Offset < 0 {
		p.Offset = 0
	}
	if p.Limit <= 0 {
		p.Limit = defaultListLimit
	}

	albums, total, err := s.uc.ListAlbums(r.Context(), p.Offset, p.Limit)
	if err != nil {
		log.Error("music listAlbums failed", "err", err)
		s.writeRPCError(w, req.ID, rpcInternalError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(albums))
	for _, a := range albums {
		out = append(out, s.albumJSON(r, a))
	}
	s.writeRPCResult(w, req.ID, map[string]any{"albums": out, "total": total})
}

func (s *MusicService) rpcListArtists(w http.ResponseWriter, r *http.Request, req *rpcRequest) {
	var p struct {
		Offset int `json:"offset"`
		Limit  int `json:"limit"`
	}
	if len(req.Params) > 0 && string(req.Params) != "null" {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			s.writeRPCError(w, req.ID, rpcInvalidParams, "invalid params")
			return
		}
	}
	if p.Offset < 0 {
		p.Offset = 0
	}
	if p.Limit <= 0 {
		p.Limit = defaultListLimit
	}

	artists, total, err := s.uc.ListArtists(r.Context(), p.Offset, p.Limit)
	if err != nil {
		log.Error("music listArtists failed", "err", err)
		s.writeRPCError(w, req.ID, rpcInternalError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(artists))
	for _, a := range artists {
		out = append(out, s.artistJSON(r, a))
	}
	s.writeRPCResult(w, req.ID, map[string]any{"artists": out, "total": total})
}

func (s *MusicService) rpcListSongs(w http.ResponseWriter, r *http.Request, req *rpcRequest) {
	var p struct {
		AlbumID  string `json:"albumId"`
		ArtistID string `json:"artistId"`
		Offset   int    `json:"offset"`
		Limit    int    `json:"limit"`
	}
	if len(req.Params) > 0 && string(req.Params) != "null" {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			s.writeRPCError(w, req.ID, rpcInvalidParams, "invalid params")
			return
		}
	}
	if p.AlbumID == "" && p.ArtistID == "" {
		s.writeRPCError(w, req.ID, rpcInvalidParams, "albumId or artistId is required")
		return
	}

	tracks, total, err := s.uc.ListSongs(r.Context(), p.AlbumID, p.ArtistID, p.Offset, p.Limit)
	if err != nil {
		if errors.Is(err, biz.ErrInvalidParams) {
			s.writeRPCError(w, req.ID, rpcInvalidParams, "invalid params")
			return
		}
		log.Error("music listSongs failed", "err", err)
		s.writeRPCError(w, req.ID, rpcInternalError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(tracks))
	for _, t := range tracks {
		out = append(out, s.trackJSON(r, t))
	}
	s.writeRPCResult(w, req.ID, map[string]any{"tracks": out, "total": total})
}

func (s *MusicService) albumJSON(r *http.Request, a *biz.Album) map[string]any {
	m := map[string]any{
		"id":        a.ID,
		"name":      a.Name,
		"artist":    a.Artist,
		"songCount": a.SongCount,
	}
	if a.Year != 0 {
		m["year"] = a.Year
	}
	if a.CoverTrackID != "" {
		m["coverUrl"] = s.coverURLForID(r, a.CoverTrackID)
	}
	return m
}

func (s *MusicService) artistJSON(r *http.Request, a *biz.Artist) map[string]any {
	m := map[string]any{
		"id":         a.ID,
		"name":       a.Name,
		"albumCount": a.AlbumCount,
		"songCount":  a.SongCount,
	}
	if a.CoverTrackID != "" {
		m["coverUrl"] = s.coverURLForID(r, a.CoverTrackID)
	}
	return m
}

func (s *MusicService) rpcStreamURL(w http.ResponseWriter, r *http.Request, req *rpcRequest) {
	var p struct {
		ID string `json:"id"`
	}
	if len(req.Params) == 0 || json.Unmarshal(req.Params, &p) != nil || p.ID == "" {
		s.writeRPCError(w, req.ID, rpcInvalidParams, "invalid params")
		return
	}
	t, err := s.uc.GetTrack(r.Context(), p.ID)
	if err != nil {
		s.writeTrackError(w, req.ID, err)
		return
	}
	s.writeRPCResult(w, req.ID, map[string]any{"url": s.streamURL(r, t)})
}

func (s *MusicService) trackJSON(r *http.Request, t *biz.Track) map[string]any {
	m := map[string]any{
		"id":         t.ID,
		"title":      t.Title,
		"artist":     t.Artist,
		"album":      t.Album,
		"durationMs": t.DurationMs,
		"url":        s.streamURL(r, t),
	}
	if t.CoverMIME != "" {
		m["coverUrl"] = s.coverURL(r, t)
	}
	if t.Lyrics != "" {
		m["lyrics"] = t.Lyrics
	}
	return m
}

func (s *MusicService) streamURL(r *http.Request, t *biz.Track) string {
	u := s.base(r) + s.streamPath + "/" + t.ID
	if s.secret != "" {
		u += "?token=" + s.signer.sign("stream", t.ID, s.ttl)
	}
	return u
}

func (s *MusicService) coverURL(r *http.Request, t *biz.Track) string {
	return s.coverURLForID(r, t.ID)
}

// coverURLForID builds a signed cover URL for an arbitrary track id. It is used
// by album/artist views, whose coverUrl points at a representative track cover.
func (s *MusicService) coverURLForID(r *http.Request, id string) string {
	u := s.base(r) + s.coverPath + "/" + id
	if s.secret != "" {
		u += "?token=" + s.signer.sign("cover", id, s.ttl)
	}
	return u
}

// --- stream / cover ---

func (s *MusicService) handleStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := mux.Vars(r)["id"]
	if id == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !s.authorizeMedia("stream", id, r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	f, t, err := s.uc.OpenStream(r.Context(), id)
	if err != nil {
		if errors.Is(err, biz.ErrTrackNotFound) {
			http.NotFound(w, r)
		} else {
			log.Error("open stream failed", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}
	defer f.Close()

	if t.MIMEType != "" {
		w.Header().Set("Content-Type", t.MIMEType)
	}
	w.Header().Set("Accept-Ranges", "bytes")
	http.ServeContent(w, r, filepath.Base(t.FilePath), t.ModTime, f)
}

func (s *MusicService) handleCover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := mux.Vars(r)["id"]
	if id == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !s.authorizeMedia("cover", id, r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	t, err := s.uc.GetTrack(r.Context(), id)
	if err != nil {
		if errors.Is(err, biz.ErrTrackNotFound) {
			http.NotFound(w, r)
		} else {
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}
	if len(t.CoverData) == 0 {
		http.NotFound(w, r)
		return
	}

	mime := t.CoverMIME
	if mime == "" {
		mime = "image/jpeg"
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("Content-Length", strconv.Itoa(len(t.CoverData)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(t.CoverData)
	}
}

// --- auth ---

// authorizeRPC enforces the optional bearer token on the JSON-RPC endpoint.
func (s *MusicService) authorizeRPC(r *http.Request) bool {
	if s.token == "" {
		return true
	}
	const prefix = "Bearer "
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, prefix) {
		return false
	}
	return hmac.Equal([]byte(auth[len(prefix):]), []byte(s.token))
}

// authorizeMedia verifies the signed token on stream/cover URLs. When signing
// is disabled it always allows.
func (s *MusicService) authorizeMedia(purpose, id string, r *http.Request) bool {
	if s.secret == "" {
		return true
	}
	return s.signer.verify(purpose, id, r.URL.Query().Get("token"), time.Now())
}

// --- helpers ---

func (s *MusicService) writeRPCResult(w http.ResponseWriter, id json.RawMessage, result any) {
	writeJSON(w, http.StatusOK, rpcResponse{JSONRPC: "2.0", ID: normalizeID(id), Result: result})
}

func (s *MusicService) writeRPCError(w http.ResponseWriter, id json.RawMessage, code int, msg string) {
	writeJSON(w, http.StatusOK, rpcResponse{
		JSONRPC: "2.0",
		ID:      normalizeID(id),
		Error:   &rpcErrorObj{Code: code, Message: msg},
	})
}

func (s *MusicService) writeTrackError(w http.ResponseWriter, id json.RawMessage, err error) {
	switch {
	case errors.Is(err, biz.ErrTrackNotFound):
		s.writeRPCError(w, id, rpcTrackNotFound, "track not found")
	case errors.Is(err, biz.ErrInvalidParams):
		s.writeRPCError(w, id, rpcInvalidParams, "invalid params")
	default:
		log.Error("music internal error", "err", err)
		s.writeRPCError(w, id, rpcInternalError, "internal error")
	}
}

func (s *MusicService) base(r *http.Request) string {
	if s.baseURL != "" {
		return s.baseURL
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if xf := r.Header.Get("X-Forwarded-Proto"); xf != "" {
		if first := strings.TrimSpace(strings.Split(xf, ",")[0]); first != "" {
			scheme = first
		}
	}
	return scheme + "://" + r.Host
}

func normalizeID(id json.RawMessage) json.RawMessage {
	if len(id) == 0 {
		return json.RawMessage("null")
	}
	return id
}

func withSlash(v, def string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		v = def
	}
	if !strings.HasPrefix(v, "/") {
		v = "/" + v
	}
	return strings.TrimRight(v, "/")
}

func readAllLimit(r *http.Request, limit int64) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	defer r.Body.Close()
	// Read one byte past the limit so we can detect an oversized body.
	b, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errors.New("body too large")
	}
	return b, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
