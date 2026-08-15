package config

// Provider resolves the current effective configuration. Implementations may
// overlay versioned runtime settings on top of the startup configuration.
type Provider interface {
	Current() *Config
}

type StaticProvider struct{ Config *Config }

func (p StaticProvider) Current() *Config { return p.Config }

// Current lets a plain *Config remain a backward-compatible static Provider.
func (c *Config) Current() *Config { return c }
