package entities

import "errors"

// Marketplace represents a plugin marketplace
type Marketplace struct {
	name           string
	url            string   // Git repository URL
	enabledPlugins []string // Plugin names (without marketplace qualifier)
}

// NewMarketplace creates a new Marketplace
func NewMarketplace(name string) *Marketplace {
	return &Marketplace{
		name:           name,
		enabledPlugins: []string{},
	}
}

// Name returns the marketplace name
func (m *Marketplace) Name() string {
	return m.name
}

// URL returns the Git repository URL
func (m *Marketplace) URL() string {
	return m.url
}

// EnabledPlugins returns the list of enabled plugin names
func (m *Marketplace) EnabledPlugins() []string {
	return m.enabledPlugins
}

// SetURL sets the Git repository URL
func (m *Marketplace) SetURL(url string) {
	m.url = url
}

// SetEnabledPlugins sets the list of enabled plugin names
func (m *Marketplace) SetEnabledPlugins(plugins []string) {
	if plugins == nil {
		m.enabledPlugins = []string{}
	} else {
		m.enabledPlugins = plugins
	}
}

// QualifiedPluginNames returns plugin names in "plugin@marketplace" format
func (m *Marketplace) QualifiedPluginNames() []string {
	result := make([]string, len(m.enabledPlugins))
	for i, plugin := range m.enabledPlugins {
		result[i] = plugin + "@" + m.name
	}
	return result
}

// Validate validates the Marketplace configuration
func (m *Marketplace) Validate() error {
	if m.name == "" {
		return errors.New("marketplace name is required")
	}
	if m.url == "" {
		return errors.New("marketplace url is required")
	}
	return nil
}

// MarketplacesSettings represents the collection of marketplaces
type MarketplacesSettings struct {
	marketplaces map[string]*Marketplace
}

// NewMarketplacesSettings creates a new MarketplacesSettings
func NewMarketplacesSettings() *MarketplacesSettings {
	return &MarketplacesSettings{
		marketplaces: make(map[string]*Marketplace),
	}
}

// Marketplaces returns all marketplaces
func (s *MarketplacesSettings) Marketplaces() map[string]*Marketplace {
	return s.marketplaces
}

// GetMarketplace returns a marketplace by name
func (s *MarketplacesSettings) GetMarketplace(name string) *Marketplace {
	return s.marketplaces[name]
}

// SetMarketplace sets a marketplace
func (s *MarketplacesSettings) SetMarketplace(name string, m *Marketplace) {
	s.marketplaces[name] = m
}

// RemoveMarketplace removes a marketplace
func (s *MarketplacesSettings) RemoveMarketplace(name string) {
	delete(s.marketplaces, name)
}

// IsEmpty returns true if there are no marketplaces
func (s *MarketplacesSettings) IsEmpty() bool {
	return len(s.marketplaces) == 0
}

// AllEnabledPlugins returns all enabled plugins from all marketplaces
// in "plugin@marketplace" format
func (s *MarketplacesSettings) AllEnabledPlugins() []string {
	var result []string
	for _, m := range s.marketplaces {
		result = append(result, m.QualifiedPluginNames()...)
	}
	return result
}

// Validate validates all marketplaces
func (s *MarketplacesSettings) Validate() error {
	for _, m := range s.marketplaces {
		if err := m.Validate(); err != nil {
			return err
		}
	}
	return nil
}
