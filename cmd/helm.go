package cmd

import "github.com/spf13/cobra"

// HelmCmd contains operational helpers for Helm based installations.
var HelmCmd = &cobra.Command{
	Use:   "helm",
	Short: "Operate agentapi-proxy Helm installations",
}

func init() {
	HelmCmd.AddCommand(newHelmMigrateCommand())
	HelmCmd.AddCommand(newHelmMigrateValuesCommand())
}
