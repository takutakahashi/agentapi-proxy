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
	Short: "Run the transparent HTTP/HTTPS proxy sidecar",
	Long: `Runs the transparent proxy sidecar that enforces the denied-domains list.

Denied domains are read from the NETWORK_FILTER_DENIED_DOMAINS environment
variable (comma-separated hostnames) or from the --denied-domains flag.

HTTP traffic (port 3128): filtered by Host header.
HTTPS traffic (port 3129): filtered by TLS SNI (no decryption).`,
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

		httpAddr := fmt.Sprintf("0.0.0.0:%d", networkfilter.HTTPProxyPort)
		httpsAddr := fmt.Sprintf("0.0.0.0:%d", networkfilter.HTTPSProxyPort)

		httpLis, err := net.Listen("tcp", httpAddr)
		if err != nil {
			return fmt.Errorf("listen HTTP %s: %w", httpAddr, err)
		}
		httpsLis, err := net.Listen("tcp", httpsAddr)
		if err != nil {
			return fmt.Errorf("listen HTTPS %s: %w", httpsAddr, err)
		}

		log.Printf("[network-filter] Starting proxy (denied domains: %v)", allDomains)

		errCh := make(chan error, 2)
		go func() { errCh <- proxy.RunHTTP(httpLis) }()
		go func() { errCh <- proxy.RunHTTPS(httpsLis) }()

		return <-errCh
	},
}

func init() {
	networkFilterProxyCmd.Flags().StringSlice("denied-domains", nil,
		"Comma-separated list of domains to block (can also be set via NETWORK_FILTER_DENIED_DOMAINS env var)")

	NetworkFilterCmd.AddCommand(networkFilterSetupCmd)
	NetworkFilterCmd.AddCommand(networkFilterProxyCmd)
}
