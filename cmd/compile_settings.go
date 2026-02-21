package cmd

import (
	"fmt"
	"log"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

var (
	compileInputPath   string
	compileOutputDir   string
	compileEnvFilePath string
	compileStartupPath string
)

var compileSettingsCmd = &cobra.Command{
	Use:   "compile-settings",
	Short: "Compile session settings YAML into configuration files",
	Long: `Compile a unified session settings YAML file into individual configuration files.

This command reads a settings YAML file and generates:
- ~/.claude.json (Claude onboarding configuration and MCP server configurations)
- ~/.claude/settings.json (Claude settings with marketplaces)
- /home/agentapi/.session/env (environment variables as KEY=VALUE)
- /home/agentapi/.session/startup.sh (startup command script)

MCP server configurations are written directly to ~/.claude.json under the "mcpServers" key,
which Claude Code reads natively without requiring a separate --mcp-config flag.

This is typically run as an init container in the session Pod.

Examples:
  # Use defaults
  agentapi-proxy helpers compile-settings

  # Custom paths
  agentapi-proxy helpers compile-settings \
    --input /session-settings/settings.yaml \
    --output-dir /home/agentapi \
    --env-file /home/agentapi/.session/env`,
	RunE: runCompileSettings,
}

func init() {
	defaults := sessionsettings.DefaultCompileOptions()
	compileSettingsCmd.Flags().StringVar(&compileInputPath, "input", defaults.InputPath,
		"Path to the session settings YAML file")
	compileSettingsCmd.Flags().StringVar(&compileOutputDir, "output-dir", defaults.OutputDir,
		"Output directory for Claude configuration files")
	compileSettingsCmd.Flags().StringVar(&compileEnvFilePath, "env-file", defaults.EnvFilePath,
		"Output path for environment variables file")
	compileSettingsCmd.Flags().StringVar(&compileStartupPath, "startup-script", defaults.StartupPath,
		"Output path for startup script")

	HelpersCmd.AddCommand(compileSettingsCmd)
}

func runCompileSettings(cmd *cobra.Command, args []string) error {
	opts := sessionsettings.CompileOptions{
		InputPath:   compileInputPath,
		OutputDir:   compileOutputDir,
		EnvFilePath: compileEnvFilePath,
		StartupPath: compileStartupPath,
	}

	log.Printf("[COMPILE-SETTINGS] Compiling settings from %s", opts.InputPath)
	if err := sessionsettings.Compile(opts); err != nil {
		return fmt.Errorf("compile-settings failed: %w", err)
	}

	fmt.Println("Session settings compiled successfully!")
	return nil
}
