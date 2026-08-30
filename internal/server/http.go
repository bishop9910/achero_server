package server

import (
	"net/http"

	"achero_server/internal/conf"
	"achero_server/internal/service"

	"github.com/go-kratos/kratos/v3/middleware/recovery"
	khttp "github.com/go-kratos/kratos/v3/transport/http"
)

// NewHTTPServer builds the HTTP server and registers the music routes.
func NewHTTPServer(c *conf.Server, music *service.MusicService) *khttp.Server {
	opts := []khttp.ServerOption{
		khttp.Middleware(recovery.Recovery()),
		khttp.Filter(corsFilter),
	}
	if c.Http.Network != "" {
		opts = append(opts, khttp.Network(c.Http.Network))
	}
	if c.Http.Addr != "" {
		opts = append(opts, khttp.Address(c.Http.Addr))
	}
	if c.Http.Timeout != nil {
		opts = append(opts, khttp.Timeout(c.Http.Timeout.AsDuration()))
	}
	srv := khttp.NewServer(opts...)
	music.RegisterHTTP(srv)
	return srv
}

// corsFilter adds permissive CORS headers so the Achero web client can call the
// RPC endpoint and stream audio, and short-circuits preflight requests.
func corsFilter(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, HEAD, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Range")
		w.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Range, Accept-Ranges, Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
