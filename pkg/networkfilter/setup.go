package networkfilter

import (
	"fmt"
	"os/exec"
)

// SidecarUID is the UID the network-filter sidecar container runs as.
// iptables rules skip packets from this UID to avoid redirect loops.
const SidecarUID = 0

// SetupIPTables configures iptables rules to redirect outbound HTTP/HTTPS
// traffic through the network-filter transparent proxy.
// Must be called with CAP_NET_ADMIN (typically from an init container).
func SetupIPTables() error {
	rules := [][]string{
		// Skip loopback traffic.
		{"-t", "nat", "-A", "OUTPUT", "-p", "tcp", "-d", "127.0.0.1", "-j", "RETURN"},
		// Skip traffic from the sidecar itself (runs as UID 0) to avoid redirect loops.
		{"-t", "nat", "-A", "OUTPUT", "-p", "tcp", "-m", "owner", "--uid-owner", fmt.Sprintf("%d", SidecarUID), "-j", "RETURN"},
		// Redirect HTTP (port 80) to transparent proxy.
		{"-t", "nat", "-A", "OUTPUT", "-p", "tcp", "--dport", "80", "-j", "REDIRECT", "--to-port", fmt.Sprintf("%d", HTTPProxyPort)},
		// Redirect HTTPS (port 443) to SNI proxy.
		{"-t", "nat", "-A", "OUTPUT", "-p", "tcp", "--dport", "443", "-j", "REDIRECT", "--to-port", fmt.Sprintf("%d", HTTPSProxyPort)},
	}
	for _, rule := range rules {
		cmd := exec.Command("iptables", rule...)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("iptables %v: %w\noutput: %s", rule, err, out)
		}
	}
	return nil
}
