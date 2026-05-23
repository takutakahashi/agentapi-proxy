package cmd

import (
	"fmt"
	"log"
	"net"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/pkg/networkfilter"
)

// NetworkFilterCmd is the root command for network-filter subcommands.
var NetworkFilterCmd = &cobra.Command{
	Use:   "network-filter",
	Short: "Network filter proxy for session sandbox",
}

var networkFilterSetupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Configure iptables rules for transparent proxy redirect (requires CAP_NET_ADMIN)",
	RunE: func(cmd *cobra.Command, args []string) error {
		log.Println("[network-filter] Setting up iptables rules...")
		if err := networkfilter.SetupIPTables(); err != nil {
			return fmt.Errorf("iptables setup failed: %w", err)
		}
		log.Println("[network-filter] iptables rules configured successfully")
		return nil
	},
}

var networkFilterProxyCmd = &cobra.Command{
	Use:   "proxy",
	Short: "Run the forward proxy sidecar",
	Long: `Runs the forward proxy sidecar that enforces domain filtering.

Allowed domains (allowlist mode) are read from NETWORK_FILTER_ALLOWED_DOMAINS env var
or --allowed-domains flag. When set, only listed domains are permitted; all others blocked.

Denied domains (denylist mode) are read from NETWORK_FILTER_DENIED_DOMAINS env var
or --denied-domains flag. When set, only listed domains are blocked.

Allowlist takes precedence when both are specified.

A single listener on port 3128 handles three types of connections:
  - HTTP CONNECT tunnels (proxy-aware HTTPS clients via HTTP_PROXY/HTTPS_PROXY env vars)
  - HTTP forward proxy requests (proxy-aware HTTP clients)
  - Transparent TLS (iptables-redirected port-443 traffic, SNI-filtered)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		allowedDomainsFlag, _ := cmd.Flags().GetStringSlice("allowed-domains")
		deniedDomainsFlag, _ := cmd.Flags().GetStringSlice("denied-domains")

		parseDomains := func(envKey string, flagVals []string) []string {
			var out []string
			if v := os.Getenv(envKey); v != "" {
				for _, d := range strings.Split(v, ",") {
					out = append(out, strings.TrimSpace(d))
				}
			}
			return append(out, flagVals...)
		}

		allowedDomains := parseDomains("NETWORK_FILTER_ALLOWED_DOMAINS", allowedDomainsFlag)
		deniedDomains := parseDomains("NETWORK_FILTER_DENIED_DOMAINS", deniedDomainsFlag)

		var filter *networkfilter.Filter
		if len(allowedDomains) > 0 {
			log.Printf("[network-filter] Starting proxy on 0.0.0.0:%d (allowlist: %v)", networkfilter.ProxyPort, allowedDomains)
			filter = networkfilter.NewAllowlistFilter(allowedDomains)
		} else {
			log.Printf("[network-filter] Starting proxy on 0.0.0.0:%d (denylist: %v)", networkfilter.ProxyPort, deniedDomains)
			filter = networkfilter.NewFilter(deniedDomains)
		}

		proxy := networkfilter.NewProxy(filter)

		addr := fmt.Sprintf("0.0.0.0:%d", networkfilter.ProxyPort)
		lis, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("listen %s: %w", addr, err)
		}

		return proxy.Run(lis)
	},
}

func init() {
	networkFilterProxyCmd.Flags().StringSlice("allowed-domains", nil,
		"Domains to allow (allowlist mode). All others blocked. Overrides denied-domains when set. Also via NETWORK_FILTER_ALLOWED_DOMAINS.")
	networkFilterProxyCmd.Flags().StringSlice("denied-domains", nil,
		"Domains to block (denylist mode). Also via NETWORK_FILTER_DENIED_DOMAINS.")

	NetworkFilterCmd.AddCommand(networkFilterSetupCmd)
	NetworkFilterCmd.AddCommand(networkFilterProxyCmd)
}
