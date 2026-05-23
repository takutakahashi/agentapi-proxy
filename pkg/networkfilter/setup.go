package networkfilter

import (
	"fmt"
	"os/exec"
)

// SidecarUID is the UID the network-filter sidecar container runs as.
// filter OUTPUT rules accept all traffic from this UID unconditionally so the
// sidecar can reach real upstreams without being self-blocked.
const SidecarUID = 0

// SetupIPTables configures iptables rules to enforce network isolation for the
// session Pod, following the same strategy as NemoClaw / NVIDIA OpenShell:
//
//   - All outbound TCP is REJECT-ed by default.
//   - Traffic from the network-filter sidecar (UID 0) is exempted so it can
//     reach real upstreams without looping back through itself.
//   - The proxy port (127.0.0.1:3128) is explicitly allowed for the main
//     container to reach the sidecar.
//   - Established / related packets (TCP replies, ICMP unreachables) are let
//     through so the proxy's upstream connections work normally.
//   - UDP is left unrestricted to avoid breaking DNS and other infrastructure;
//     add explicit UDP rules here if stricter UDP isolation is required.
//
// Primary mechanism: HTTP_PROXY / HTTPS_PROXY environment variables (injected
// by the session manager) make proxy-aware clients (curl, Go http.Client, npm,
// pip, etc.) use CONNECT tunnels for ALL ports, not just 80/443. The REJECT
// rules below ensure non-proxy-aware direct TCP connections are also blocked.
//
// Must be called with CAP_NET_ADMIN — from the network-filter-setup init container.
func SetupIPTables() error {
	proxyPort := fmt.Sprintf("%d", ProxyPort)
	sidecarUID := fmt.Sprintf("%d", SidecarUID)

	rules := [][]string{
		// ── filter OUTPUT (primary enforcement) ────────────────────────────────

		// Allow loopback.
		{"-t", "filter", "-A", "OUTPUT", "-o", "lo", "-j", "ACCEPT"},

		// Allow all traffic from the sidecar (UID 0) so it can reach upstreams.
		{"-t", "filter", "-A", "OUTPUT", "-m", "owner", "--uid-owner", sidecarUID, "-j", "ACCEPT"},

		// Allow main container to reach the local proxy sidecar.
		{"-t", "filter", "-A", "OUTPUT", "-p", "tcp", "-d", "127.0.0.1", "--dport", proxyPort, "-j", "ACCEPT"},

		// Allow established / related packets (responses from sidecar's upstream connections).
		{"-t", "filter", "-A", "OUTPUT", "-m", "state", "--state", "ESTABLISHED,RELATED", "-j", "ACCEPT"},

		// REJECT all other outbound TCP with a TCP RST (fast fail for the client).
		{"-t", "filter", "-A", "OUTPUT", "-p", "tcp", "-j", "REJECT", "--reject-with", "tcp-reset"},

		// ── nat OUTPUT (transparent interception fallback) ─────────────────────
		// Redirect port 80/443 to the sidecar for clients that ignore HTTP_PROXY
		// and open a direct TCP connection. These connections would be blocked by
		// the filter REJECT above, but redirecting them first lets the sidecar
		// serve an informative 403 instead of a bare RST.

		// Skip loopback in NAT (after filter rules; order matters less here).
		{"-t", "nat", "-A", "OUTPUT", "-p", "tcp", "-d", "127.0.0.1", "-j", "RETURN"},
		// Skip sidecar traffic in NAT to avoid loops.
		{"-t", "nat", "-A", "OUTPUT", "-p", "tcp", "-m", "owner", "--uid-owner", sidecarUID, "-j", "RETURN"},
		// Redirect HTTP.
		{"-t", "nat", "-A", "OUTPUT", "-p", "tcp", "--dport", "80", "-j", "REDIRECT", "--to-port", proxyPort},
		// Redirect HTTPS.
		{"-t", "nat", "-A", "OUTPUT", "-p", "tcp", "--dport", "443", "-j", "REDIRECT", "--to-port", proxyPort},
	}

	for _, rule := range rules {
		cmd := exec.Command("iptables", rule...)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("iptables %v: %w\noutput: %s", rule, err, out)
		}
	}
	return nil
}
