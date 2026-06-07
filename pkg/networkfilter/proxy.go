package networkfilter

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// ProxyPort is the port the forward proxy listens on.
	// It serves both HTTP forward proxy requests and HTTP CONNECT tunnels.
	// Transparent iptables-redirected connections (port 80/443) are also accepted here.
	ProxyPort = 3128

	// ControlPort is the port the control server listens on (localhost only).
	// Used to enable the policy after startup via POST /enable-policy.
	ControlPort = 3129

	dialTimeout = 10 * time.Second
)

// passthroughFilter allows all traffic (empty denylist, no allowlist).
var passthroughFilter = &Filter{}

// DomainLog tracks which domains were accessed (allowed) or rejected (denied) by the proxy.
type DomainLog struct {
	mu      sync.RWMutex
	allowed map[string]struct{}
	denied  map[string]struct{}
}

func newDomainLog() *DomainLog {
	return &DomainLog{
		allowed: make(map[string]struct{}),
		denied:  make(map[string]struct{}),
	}
}

func (d *DomainLog) recordAllowed(domain string) {
	d.mu.Lock()
	d.allowed[domain] = struct{}{}
	d.mu.Unlock()
}

func (d *DomainLog) recordDenied(domain string) {
	d.mu.Lock()
	d.denied[domain] = struct{}{}
	d.mu.Unlock()
}

// Snapshot returns a copy of the current allowed and denied domain sets.
func (d *DomainLog) Snapshot() (allowed, denied []string) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	for k := range d.allowed {
		allowed = append(allowed, k)
	}
	for k := range d.denied {
		denied = append(denied, k)
	}
	return
}

// Proxy is a forward proxy that enforces a domain deny-list.
// It handles three types of incoming connections on a single port:
//
//  1. HTTP CONNECT tunnels   — sent by proxy-aware HTTPS clients (via HTTP_PROXY env var)
//  2. HTTP forward requests  — sent by proxy-aware HTTP clients  (via HTTP_PROXY env var)
//  3. Transparent TCP        — redirected by iptables from port 80/443
//     - port 80:  parsed as an HTTP request (Host header)
//     - port 443: TLS ClientHello SNI peek
//
// When created with active=false (deferred mode), all traffic is allowed until
// EnablePolicy is called.
type Proxy struct {
	configuredFilter atomic.Pointer[Filter]
	activeFilter     atomic.Pointer[Filter]
	policyActive     atomic.Bool
	countMode        atomic.Bool
	domainLog        *DomainLog
}

// NewProxy creates a Proxy with the given filter.
// When active is true, the policy is enforced immediately.
// When active is false, all traffic is allowed until EnablePolicy is called.
func NewProxy(filter *Filter, active bool, countMode bool) *Proxy {
	p := &Proxy{
		domainLog: newDomainLog(),
	}
	p.configuredFilter.Store(filter)
	p.countMode.Store(countMode)
	if active {
		p.activeFilter.Store(filter)
		p.policyActive.Store(true)
	}
	return p
}

// Domains returns the current snapshot of accessed (allowed) and rejected (denied) domains.
func (p *Proxy) Domains() (allowed, denied []string) {
	return p.domainLog.Snapshot()
}

// EnablePolicy activates the configured filter. After this call, all connections
// are subject to domain filtering. Safe to call concurrently.
func (p *Proxy) EnablePolicy() {
	p.activeFilter.Store(p.configuredFilter.Load())
	p.policyActive.Store(true)
	log.Printf("[network-filter] policy enabled")
}

// SetPolicy replaces the configured filter and count-mode behavior. If the
// policy is already active, the new policy takes effect immediately.
func (p *Proxy) SetPolicy(filter *Filter, countMode bool) {
	p.configuredFilter.Store(filter)
	p.countMode.Store(countMode)
	if p.policyActive.Load() {
		p.activeFilter.Store(filter)
	}
	log.Printf("[network-filter] policy configured (count_mode=%t)", countMode)
}

// effectiveFilter returns the active filter, or the passthrough filter if the policy
// has not yet been enabled.
func (p *Proxy) effectiveFilter() *Filter {
	if f := p.activeFilter.Load(); f != nil {
		return f
	}
	return passthroughFilter
}

func (p *Proxy) shouldBlock(result FilterResult) bool {
	return result == FilterResultBlocked && !p.countMode.Load()
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
		host = req.Host
	}

	result := p.effectiveFilter().Check(host)
	log.Printf("[network-filter] CONNECT %s: %s", result, req.Host)

	if result == FilterResultBlocked {
		p.domainLog.recordDenied(host)
	} else {
		p.domainLog.recordAllowed(host)
	}

	if p.shouldBlock(result) {
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

	result := p.effectiveFilter().Check(host)
	log.Printf("[network-filter] HTTP %s: %s", result, host)

	if result == FilterResultBlocked {
		p.domainLog.recordDenied(host)
	} else {
		p.domainLog.recordAllowed(host)
	}

	if p.shouldBlock(result) {
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

	result := p.effectiveFilter().Check(sni)
	log.Printf("[network-filter] TLS %s: %s", result, sni)

	if result == FilterResultBlocked {
		p.domainLog.recordDenied(sni)
	} else {
		p.domainLog.recordAllowed(sni)
	}

	if p.shouldBlock(result) {
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
