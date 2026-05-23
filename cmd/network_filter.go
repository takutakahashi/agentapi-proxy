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
	Long: `Runs the forward proxy sidecar that enforces the denied-domains list.

Denied domains are read from the NETWORK_FILTER_DENIED_DOMAINS environment
variable (comma-separated hostnames) or from the --denied-domains flag.

A single listener on port 3128 handles three types of connections:
  - HTTP CONNECT tunnels (proxy-aware HTTPS clients via HTTP_PROXY/HTTPS_PROXY env vars)
  - HTTP forward proxy requests (proxy-aware HTTP clients)
  - Transparent TLS (iptables-redirected port-443 traffic, SNI-filtered)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		deniedDomainsFlag, _ := cmd.Flags().GetStringSlice("denied-domains")

		// Merge flag values with the environment variable.
		envVal := os.Getenv("NETWORK_FILTER_DENIED_DOMAINS")
		var allDomains []string
		if envVal != "" {
			for _, d := range strings.Split(envVal, ",") {
				allDomains = append(allDomains, strings.TrimSpace(d))
			}
		}
		allDomains = append(allDomains, deniedDomainsFlag...)

		filter := networkfilter.NewFilter(allDomains)
		proxy := networkfilter.NewProxy(filter)

		addr := fmt.Sprintf("0.0.0.0:%d", networkfilter.ProxyPort)
		lis, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("listen %s: %w", addr, err)
		}

		log.Printf("[network-filter] Starting proxy on %s (denied domains: %v)", addr, allDomains)
		return proxy.Run(lis)
	},
}

func init() {
	networkFilterProxyCmd.Flags().StringSlice("denied-domains", nil,
		"Comma-separated list of domains to block (can also be set via NETWORK_FILTER_DENIED_DOMAINS env var)")

	NetworkFilterCmd.AddCommand(networkFilterSetupCmd)
	NetworkFilterCmd.AddCommand(networkFilterProxyCmd)
}
