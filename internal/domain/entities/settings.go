package entities

import (
	"errors"
	"time"
)

// BedrockSettings represents AWS Bedrock configuration
type BedrockSettings struct {
	enabled         bool
	model           string
	accessKeyID     string
	secretAccessKey string
	roleARN         string
	profile         string
}

// NewBedrockSettings creates a new BedrockSettings
func NewBedrockSettings(enabled bool) *BedrockSettings {
	return &BedrockSettings{
		enabled: enabled,
	}
}

// Enabled returns whether Bedrock is enabled
func (b *BedrockSettings) Enabled() bool {
	return b.enabled
}

// Model returns the model ID
func (b *BedrockSettings) Model() string {
	return b.model
}

// AccessKeyID returns the AWS access key ID
func (b *BedrockSettings) AccessKeyID() string {
	return b.accessKeyID
}

// SecretAccessKey returns the AWS secret access key
func (b *BedrockSettings) SecretAccessKey() string {
	return b.secretAccessKey
}

// RoleARN returns the AWS role ARN for AssumeRole
func (b *BedrockSettings) RoleARN() string {
	return b.roleARN
}

// Profile returns the AWS profile name
func (b *BedrockSettings) Profile() string {
	return b.profile
}

// SetModel sets the model ID
func (b *BedrockSettings) SetModel(model string) {
	b.model = model
}

// SetAccessKeyID sets the AWS access key ID
func (b *BedrockSettings) SetAccessKeyID(accessKeyID string) {
	b.accessKeyID = accessKeyID
}

// SetSecretAccessKey sets the AWS secret access key
func (b *BedrockSettings) SetSecretAccessKey(secretAccessKey string) {
	b.secretAccessKey = secretAccessKey
}

// SetRoleARN sets the AWS role ARN
func (b *BedrockSettings) SetRoleARN(roleARN string) {
	b.roleARN = roleARN
}

// SetProfile sets the AWS profile name
func (b *BedrockSettings) SetProfile(profile string) {
	b.profile = profile
}

// Validate validates the BedrockSettings
func (b *BedrockSettings) Validate() error {
	return nil
}

// Settings represents user or team settings
type Settings struct {
	name           string
	bedrock        *BedrockSettings
	mcpServers     *MCPServersSettings
	marketplaces   *MarketplacesSettings
	enabledPlugins []string // plugin@marketplace format (e.g., "commit@claude-plugins-official")
	createdAt      time.Time
	updatedAt      time.Time
}

// NewSettings creates a new Settings
func NewSettings(name string) *Settings {
	now := time.Now()
	return &Settings{
		name:      name,
		createdAt: now,
		updatedAt: now,
	}
}

// Name returns the settings name (user or team name)
func (s *Settings) Name() string {
	return s.name
}

// Bedrock returns the Bedrock settings
func (s *Settings) Bedrock() *BedrockSettings {
	return s.bedrock
}

// CreatedAt returns the creation time
func (s *Settings) CreatedAt() time.Time {
	return s.createdAt
}

// UpdatedAt returns the last update time
func (s *Settings) UpdatedAt() time.Time {
	return s.updatedAt
}

// SetBedrock sets the Bedrock settings
func (s *Settings) SetBedrock(bedrock *BedrockSettings) {
	s.bedrock = bedrock
	s.updatedAt = time.Now()
}

// MCPServers returns the MCP servers settings
func (s *Settings) MCPServers() *MCPServersSettings {
	return s.mcpServers
}

// SetMCPServers sets the MCP servers settings
func (s *Settings) SetMCPServers(mcpServers *MCPServersSettings) {
	s.mcpServers = mcpServers
	s.updatedAt = time.Now()
}

// Marketplaces returns the marketplaces settings
func (s *Settings) Marketplaces() *MarketplacesSettings {
	return s.marketplaces
}

// SetMarketplaces sets the marketplaces settings
func (s *Settings) SetMarketplaces(marketplaces *MarketplacesSettings) {
	s.marketplaces = marketplaces
	s.updatedAt = time.Now()
}

// EnabledPlugins returns the list of enabled plugins in "plugin@marketplace" format
func (s *Settings) EnabledPlugins() []string {
	return s.enabledPlugins
}

// SetEnabledPlugins sets the list of enabled plugins in "plugin@marketplace" format
func (s *Settings) SetEnabledPlugins(plugins []string) {
	s.enabledPlugins = plugins
	s.updatedAt = time.Now()
}

// SetCreatedAt sets the creation time (for loading from storage)
func (s *Settings) SetCreatedAt(t time.Time) {
	s.createdAt = t
}

// SetUpdatedAt sets the update time (for loading from storage)
func (s *Settings) SetUpdatedAt(t time.Time) {
	s.updatedAt = t
}

// Validate validates the Settings
func (s *Settings) Validate() error {
	if s.name == "" {
		return errors.New("name is required")
	}

	if s.bedrock != nil {
		if err := s.bedrock.Validate(); err != nil {
			return err
		}
	}

	if s.mcpServers != nil {
		if err := s.mcpServers.Validate(); err != nil {
			return err
		}
	}

	if s.marketplaces != nil {
		if err := s.marketplaces.Validate(); err != nil {
			return err
		}
	}

	return nil
}
