package entities

import (
	"errors"
	"sort"
)

// MCPServer represents a single MCP server configuration
type MCPServer struct {
	name    string
	type_   string            // "stdio", "http", "sse"
	url     string            // for http/sse
	command string            // for stdio
	args    []string          // for stdio
	env     map[string]string // environment variables (may contain secrets)
	headers map[string]string // for http/sse (may contain secrets)
}

// NewMCPServer creates a new MCPServer
func NewMCPServer(name string, serverType string) *MCPServer {
	return &MCPServer{
		name:    name,
		type_:   serverType,
		args:    []string{},
		env:     make(map[string]string),
		headers: make(map[string]string),
	}
}

// Name returns the server name
func (s *MCPServer) Name() string {
	return s.name
}

// Type returns the server type
func (s *MCPServer) Type() string {
	return s.type_
}

// URL returns the URL for http/sse servers
func (s *MCPServer) URL() string {
	return s.url
}

// Command returns the command for stdio servers
func (s *MCPServer) Command() string {
	return s.command
}

// Args returns the arguments for stdio servers
func (s *MCPServer) Args() []string {
	return s.args
}

// Env returns the environment variables
func (s *MCPServer) Env() map[string]string {
	return s.env
}

// Headers returns the headers for http/sse servers
func (s *MCPServer) Headers() map[string]string {
	return s.headers
}

// SetURL sets the URL
func (s *MCPServer) SetURL(url string) {
	s.url = url
}

// SetCommand sets the command
func (s *MCPServer) SetCommand(command string) {
	s.command = command
}

// SetArgs sets the arguments
func (s *MCPServer) SetArgs(args []string) {
	if args == nil {
		s.args = []string{}
	} else {
		s.args = args
	}
}

// SetEnv sets the environment variables
func (s *MCPServer) SetEnv(env map[string]string) {
	if env == nil {
		s.env = make(map[string]string)
	} else {
		s.env = env
	}
}

// SetHeaders sets the headers
func (s *MCPServer) SetHeaders(headers map[string]string) {
	if headers == nil {
		s.headers = make(map[string]string)
	} else {
		s.headers = headers
	}
}

// Validate validates the MCPServer configuration
func (s *MCPServer) Validate() error {
	if s.name == "" {
		return errors.New("server name is required")
	}

	switch s.type_ {
	case "stdio":
		if s.command == "" {
			return errors.New("command is required for stdio server")
		}
	case "http", "sse":
		if s.url == "" {
			return errors.New("url is required for http/sse server")
		}
	case "":
		return errors.New("server type is required")
	default:
		return errors.New("invalid server type: must be stdio, http, or sse")
	}

	return nil
}

// EnvKeys returns sorted list of environment variable keys
func (s *MCPServer) EnvKeys() []string {
	if len(s.env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(s.env))
	for k := range s.env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// HeaderKeys returns sorted list of header keys
func (s *MCPServer) HeaderKeys() []string {
	if len(s.headers) == 0 {
		return nil
	}
	keys := make([]string, 0, len(s.headers))
	for k := range s.headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// MCPServersSettings represents MCP servers configuration
type MCPServersSettings struct {
	servers map[string]*MCPServer
}

// NewMCPServersSettings creates a new MCPServersSettings
func NewMCPServersSettings() *MCPServersSettings {
	return &MCPServersSettings{
		servers: make(map[string]*MCPServer),
	}
}

// Servers returns all servers
func (m *MCPServersSettings) Servers() map[string]*MCPServer {
	return m.servers
}

// GetServer returns a server by name
func (m *MCPServersSettings) GetServer(name string) *MCPServer {
	return m.servers[name]
}

// SetServer sets a server
func (m *MCPServersSettings) SetServer(name string, server *MCPServer) {
	m.servers[name] = server
}

// RemoveServer removes a server
func (m *MCPServersSettings) RemoveServer(name string) {
	delete(m.servers, name)
}

// ServerNames returns sorted list of server names
func (m *MCPServersSettings) ServerNames() []string {
	names := make([]string, 0, len(m.servers))
	for name := range m.servers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// IsEmpty returns true if there are no servers
func (m *MCPServersSettings) IsEmpty() bool {
	return len(m.servers) == 0
}

// Validate validates all servers
func (m *MCPServersSettings) Validate() error {
	for _, server := range m.servers {
		if err := server.Validate(); err != nil {
			return err
		}
	}
	return nil
}
