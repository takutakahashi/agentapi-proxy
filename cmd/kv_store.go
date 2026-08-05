package cmd

import "github.com/spf13/cobra"

// KVStoreCmd contains operational helpers for the application KV store.
var KVStoreCmd = &cobra.Command{
	Use:   "kv-store",
	Short: "Operate the application KV store",
}

func init() {
	KVStoreCmd.AddCommand(newKVStoreMigrateCommand())
}
