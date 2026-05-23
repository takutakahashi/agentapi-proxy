package networkfilter

import (
	"fmt"
	"os/exec"
)

// SidecarUID is the UID the network-filter sidecar container runs as.
// iptables OUTPUT rules skip packets from this UID to prevent redirect loops:
// the sidecar connects to real upstreams and those packets must not be re-intercepted.
const SidecarUID = 0

// SetupIPTables configures iptables NAT OUTPUT rules so that outbound HTTP and
// HTTPS traffic from the main container is redirected to the network-filter sidecar.
//
// Primary filtering mechanism: HTTP_PROXY / HTTPS_PROXY env vars (injected by the
// session manager) make proxy-aware clients use CONNECT tunnels so the sidecar sees
// the target hostname directly — no TLS decryption required.
//
// This iptables redirect is a belt-and-suspenders fallback for clients that do not
// respect HTTP_PROXY (e.g. programs that open raw TCP sockets to port 80/443).
//
// Must be called with CAP_NET_ADMIN — typically from an init container.
func SetupIPTables() error {
	rules := [][]string{
		// Skip loopback — keeps localhost communication intact.
		{"-t", "nat", "-A", "OUTPUT", "-p", "tcp", "-d", "127.0.0.1", "-j", "RETURN"},
		// Skip traffic from the sidecar itself (UID 0) to avoid redirect loops.
		{"-t", "nat", "-A", "OUTPUT", "-p", "tcp", "-m", "owner", "--uid-owner", fmt.Sprintf("%d", SidecarUID), "-j", "RETURN"},
		// Redirect outbound HTTP (port 80) to the forward proxy.
		{"-t", "nat", "-A", "OUTPUT", "-p", "tcp", "--dport", "80", "-j", "REDIRECT", "--to-port", fmt.Sprintf("%d", ProxyPort)},
		// Redirect outbound HTTPS (port 443) to the forward proxy.
		// The proxy peeks at the TLS ClientHello SNI for clients that bypass HTTP_PROXY.
		{"-t", "nat", "-A", "OUTPUT", "-p", "tcp", "--dport", "443", "-j", "REDIRECT", "--to-port", fmt.Sprintf("%d", ProxyPort)},
	}
	for _, rule := range rules {
		cmd := exec.Command("iptables", rule...)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("iptables %v: %w\noutput: %s", rule, err, out)
		}
	}
	return nil
}
