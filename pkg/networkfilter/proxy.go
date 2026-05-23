package networkfilter

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	// ProxyPort is the port the forward proxy listens on.
	// It serves both HTTP forward proxy requests and HTTP CONNECT tunnels.
	// Transparent iptables-redirected connections (port 80/443) are also accepted here.
	ProxyPort = 3128

	dialTimeout = 10 * time.Second
)

// Proxy is a forward proxy that enforces a domain deny-list.
// It handles three types of incoming connections on a single port:
//
//  1. HTTP CONNECT tunnels   — sent by proxy-aware HTTPS clients (via HTTP_PROXY env var)
//  2. HTTP forward requests  — sent by proxy-aware HTTP clients  (via HTTP_PROXY env var)
//  3. Transparent TCP        — redirected by iptables from port 80/443
//     - port 80:  parsed as an HTTP request (Host header)
//     - port 443: TLS ClientHello SNI peek
type Proxy struct {
	filter *Filter
}

// NewProxy creates a Proxy with the given deny-list filter.
func NewProxy(filter *Filter) *Proxy {
	return &Proxy{filter: filter}
}

// Run starts the proxy on the given listener.
// It blocks until lis is closed.
func (p *Proxy) Run(lis net.Listener) error {
	log.Printf("[network-filter] proxy listening on %s", lis.Addr())
	for {
		conn, err := lis.Accept()
		if err != nil {
			return err
		}
		go p.handle(conn)
	}
}

// handle dispatches an incoming connection by inspecting the first bytes.
func (p *Proxy) handle(conn net.Conn) {
	defer conn.Close() //nolint:errcheck

	br := bufio.NewReaderSize(conn, 4096)

	// Peek at the first byte to decide the protocol.
	first, err := br.Peek(1)
	if err != nil {
		return
	}

	// TLS ClientHello starts with 0x16 (TLS record type: handshake).
	// These arrive when iptables redirects port-443 traffic transparently.
	if first[0] == 0x16 {
		p.handleTransparentTLS(conn, br)
		return
	}

	// Everything else is treated as an HTTP request (forward proxy or transparent HTTP).
	req, err := http.ReadRequest(br)
	if err != nil {
		return
	}

	if req.Method == http.MethodConnect {
		p.handleCONNECT(conn, req)
	} else {
		p.handleHTTP(conn, br, req)
	}
}

// handleCONNECT processes HTTP CONNECT tunnel requests.
// This is the standard way proxy-aware clients (curl, browsers, Go's http.Client)
// establish HTTPS connections through a forward proxy.
// The target hostname is taken directly from the CONNECT request line — no SNI peeking needed.
func (p *Proxy) handleCONNECT(conn net.Conn, req *http.Request) {
	host, _, err := net.SplitHostPort(req.Host)
	if err != nil {
		// No port in CONNECT target — use as-is for domain matching.
		host = req.Host
	}

	if p.filter.IsDenied(host) {
		log.Printf("[network-filter] CONNECT blocked: %s", req.Host)
		_, _ = fmt.Fprintf(conn, "HTTP/1.1 403 Forbidden\r\n\r\nblocked by network filter\n")
		return
	}

	upConn, err := net.DialTimeout("tcp", req.Host, dialTimeout)
	if err != nil {
		log.Printf("[network-filter] CONNECT dial error %s: %v", req.Host, err)
		_, _ = fmt.Fprintf(conn, "HTTP/1.1 502 Bad Gateway\r\n\r\n%v\n", err)
		return
	}
	defer upConn.Close() //nolint:errcheck

	_, _ = fmt.Fprint(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
	pipe(conn, upConn)
}

// handleHTTP processes a direct HTTP forward proxy request or an iptables-redirected
// HTTP connection (port 80 → 3128). The host is taken from the Host header.
func (p *Proxy) handleHTTP(conn net.Conn, br *bufio.Reader, req *http.Request) {
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
			Header:     http.Header{"Content-Type": []string{"text/plain"}, "Connection": []string{"close"}},
			Body:       io.NopCloser(strings.NewReader("blocked by network filter\n")),
		}
		_ = resp.Write(conn)
		return
	}

	upstream := req.Host
	if upstream == "" {
		upstream = req.URL.Host
	}
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
	defer upConn.Close() //nolint:errcheck

	// Re-write the request in origin server format (strip absolute URL).
	req.RequestURI = req.URL.RequestURI()
	if err := req.Write(upConn); err != nil {
		return
	}
	pipe(conn, upConn)
}

// handleTransparentTLS handles TLS connections redirected transparently by iptables
// (port 443 → 3128).  It peeks at the TLS ClientHello to extract the SNI hostname
// without decrypting the traffic, then either closes the connection (denied) or
// pipes it through to the real upstream.
func (p *Proxy) handleTransparentTLS(conn net.Conn, br *bufio.Reader) {
	sni, raw, err := PeekSNI(br)
	if err != nil {
		// Cannot determine hostname — fail-open to avoid blocking legitimate traffic.
		log.Printf("[network-filter] TLS: SNI parse error (fail-open): %v", err)
		// Without knowing the destination we can't forward; just close.
		return
	}

	if p.filter.IsDenied(sni) {
		log.Printf("[network-filter] TLS blocked: %s", sni)
		return
	}

	upstream := net.JoinHostPort(sni, "443")
	upConn, err := net.DialTimeout("tcp", upstream, dialTimeout)
	if err != nil {
		log.Printf("[network-filter] TLS dial error %s: %v", upstream, err)
		return
	}
	defer upConn.Close() //nolint:errcheck

	// Replay the already-peeked bytes before piping.
	if _, err := upConn.Write(raw); err != nil {
		return
	}
	pipe(io.MultiReader(br, conn), upConn, conn)
}

// pipe copies bytes bidirectionally between two connections until either side closes.
// Optional extra writers receive the same bytes as the second connection sends back.
func pipe(a, b interface{ Read([]byte) (int, error) }, extras ...io.Writer) {
	aConn, aOK := a.(net.Conn)
	bConn, bOK := b.(net.Conn)
	if !aOK || !bOK {
		// Fallback for non-Conn readers (e.g. io.MultiReader).
		done := make(chan struct{}, 2)
		go func() { _, _ = io.Copy(b.(io.Writer), a.(io.Reader)); done <- struct{}{} }()
		go func() {
			if len(extras) > 0 {
				_, _ = io.Copy(io.MultiWriter(extras...), b.(io.Reader))
			} else {
				_, _ = io.Copy(a.(io.Writer), b.(io.Reader))
			}
			done <- struct{}{}
		}()
		<-done
		return
	}

	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(bConn, aConn); done <- struct{}{} }()
	go func() { _, _ = io.Copy(aConn, bConn); done <- struct{}{} }()
	<-done
}
