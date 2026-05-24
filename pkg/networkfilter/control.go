package networkfilter

import (
	"fmt"
	"log"
	"net"
	"net/http"
)

// ControlServer exposes a minimal HTTP API for managing the proxy at runtime.
// It listens on ControlPort (localhost only) and accepts:
//
//   - POST /enable-policy  — activate the configured filter (idempotent)
type ControlServer struct {
	proxy *Proxy
}

// NewControlServer creates a ControlServer that operates on the given proxy.
func NewControlServer(proxy *Proxy) *ControlServer {
	return &ControlServer{proxy: proxy}
}

// Run starts the HTTP control server on lis and blocks until lis is closed.
func (c *ControlServer) Run(lis net.Listener) error {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /enable-policy", func(w http.ResponseWriter, _ *http.Request) {
		c.proxy.EnablePolicy()
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "policy enabled")
	})
	srv := &http.Server{Handler: mux}
	log.Printf("[network-filter] control server listening on %s", lis.Addr())
	return srv.Serve(lis)
}
