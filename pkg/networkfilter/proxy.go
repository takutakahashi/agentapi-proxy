package networkfilter

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	// HTTPProxyPort is the port the transparent HTTP proxy listens on.
	HTTPProxyPort = 3128
	// HTTPSProxyPort is the port the transparent HTTPS (SNI) proxy listens on.
	HTTPSProxyPort = 3129

	dialTimeout = 10 * time.Second
)

// Proxy is a transparent HTTP/HTTPS proxy that enforces a domain deny-list.
type Proxy struct {
	filter *Filter
}

// NewProxy creates a Proxy with the given deny-list filter.
func NewProxy(filter *Filter) *Proxy {
	return &Proxy{filter: filter}
}

// RunHTTP starts the transparent HTTP proxy on the given listener.
// It blocks until lis is closed.
func (p *Proxy) RunHTTP(lis net.Listener) error {
	log.Printf("[network-filter] HTTP proxy listening on %s", lis.Addr())
	for {
		conn, err := lis.Accept()
		if err != nil {
			return err
		}
		go p.handleHTTP(conn)
	}
}

// RunHTTPS starts the transparent HTTPS (SNI-based) proxy on the given listener.
// It blocks until lis is closed.
func (p *Proxy) RunHTTPS(lis net.Listener) error {
	log.Printf("[network-filter] HTTPS proxy listening on %s", lis.Addr())
	for {
		conn, err := lis.Accept()
		if err != nil {
			return err
		}
		go p.handleHTTPS(conn)
	}
}

// handleHTTP reads one HTTP request, checks the Host header, and either blocks or forwards.
func (p *Proxy) handleHTTP(conn net.Conn) {
	defer conn.Close()

	br := bufio.NewReader(conn)
	req, err := http.ReadRequest(br)
	if err != nil {
		return
	}
	host := req.Host
	if host == "" {
		host = req.URL.Host
	}

	if p.filter.IsDenied(host) {
		log.Printf("[network-filter] HTTP blocked: %s", host)
		resp := &http.Response{
			Status:     "403 Forbidden",
			StatusCode: http.StatusForbidden,
			Proto:      "HTTP/1.1",
			ProtoMajor: 1,
			ProtoMinor: 1,
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Body:       io.NopCloser(strings.NewReader("blocked by network filter\n")),
		}
		_ = resp.Write(conn)
		return
	}

	// Determine upstream address.
	upstream := host
	if !strings.Contains(upstream, ":") {
		upstream = net.JoinHostPort(upstream, "80")
	}

	upConn, err := net.DialTimeout("tcp", upstream, dialTimeout)
	if err != nil {
		log.Printf("[network-filter] HTTP dial error %s: %v", upstream, err)
		resp := &http.Response{
			Status:     "502 Bad Gateway",
			StatusCode: http.StatusBadGateway,
			Proto:      "HTTP/1.1",
			ProtoMajor: 1,
			ProtoMinor: 1,
			Body:       io.NopCloser(strings.NewReader(fmt.Sprintf("dial error: %v\n", err))),
		}
		_ = resp.Write(conn)
		return
	}
	defer upConn.Close()

	// Forward original request.
	if err := req.Write(upConn); err != nil {
		return
	}
	// Drain any buffered bytes from br back into the pipe.
	if br.Buffered() > 0 {
		buf := make([]byte, br.Buffered())
		_, _ = br.Read(buf)
		_, _ = upConn.Write(buf)
	}

	// Bi-directional copy.
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(upConn, conn); done <- struct{}{} }()
	go func() { _, _ = io.Copy(conn, upConn); done <- struct{}{} }()
	<-done
}

// handleHTTPS peeks at the TLS ClientHello to extract the SNI hostname,
// checks it against the deny-list, and either closes the connection or
// pipes it through transparently (without decryption).
func (p *Proxy) handleHTTPS(conn net.Conn) {
	defer conn.Close()

	br := bufio.NewReader(conn)
	sni, raw, err := PeekSNI(br)
	if err != nil {
		// Could not parse SNI — let traffic through (fail-open for non-TLS or unusual clients).
		log.Printf("[network-filter] HTTPS: SNI parse error (fail-open): %v", err)
		p.pipeHTTPS(conn, br, raw, conn.RemoteAddr().String())
		return
	}

	if p.filter.IsDenied(sni) {
		log.Printf("[network-filter] HTTPS blocked: %s", sni)
		return // close connection — sends TCP RST / FIN
	}

	upstream := net.JoinHostPort(sni, "443")
	p.pipeHTTPS(conn, br, raw, upstream)
}

// pipeHTTPS replays raw (already-read bytes) followed by the rest of conn to upstream.
func (p *Proxy) pipeHTTPS(conn net.Conn, br *bufio.Reader, raw []byte, upstream string) {
	upConn, err := net.DialTimeout("tcp", upstream, dialTimeout)
	if err != nil {
		log.Printf("[network-filter] HTTPS dial error %s: %v", upstream, err)
		return
	}
	defer upConn.Close()

	// Replay the already-read bytes (TLS record).
	if len(raw) > 0 {
		if _, err := upConn.Write(raw); err != nil {
			return
		}
	}
	// Forward any additional buffered bytes.
	if br.Buffered() > 0 {
		buf := make([]byte, br.Buffered())
		n, _ := br.Read(buf)
		if n > 0 {
			if _, err := upConn.Write(buf[:n]); err != nil {
				return
			}
		}
	}

	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(upConn, io.MultiReader(bytes.NewReader(nil), conn))
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(conn, upConn)
		done <- struct{}{}
	}()
	<-done
}
