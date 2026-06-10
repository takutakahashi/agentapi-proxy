package mcpmodule

import (
	"log"

	"github.com/takutakahashi/agentapi-proxy/internal/app/modules/modulehost"
	mcpiface "github.com/takutakahashi/agentapi-proxy/internal/interfaces/mcp"
)

// RegisterHandler registers the MCP HTTP handler.
func RegisterHandler(proxyServer modulehost.MCPHost) {
	log.Printf("[MCP_HANDLER] Registering MCP handler...")

	mcpHandler := mcpiface.NewMCPHandler(proxyServer)
	proxyServer.AddCustomHandler(mcpHandler)

	log.Printf("[MCP_HANDLER] MCP handler registered successfully at /mcp")
}
