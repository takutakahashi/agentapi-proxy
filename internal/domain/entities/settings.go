package entities

import (
	"errors"
	"time"
)

// BedrockSettings represents AWS Bedrock configuration
type BedrockSettings struct {
	enabled         bool
	region          string
	model           string
	accessKeyID     string
	secretAccessKey string
	roleARN         string
	profile         string
}

// NewBedrockSettings creates a new BedrockSettings
func NewBedrockSettings(enabled bool, region string) *BedrockSettings {
	return &BedrockSettings{
		enabled: enabled,
		region:  region,
	}
}

// Enabled returns whether Bedrock is enabled
func (b *BedrockSettings) Enabled() bool {
	return b.enabled
}

// Region returns the AWS region
func (b *BedrockSettings) Region() string {
	return b.region
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
	if !b.enabled {
		return nil
	}

	if b.region == "" {
		return errors.New("region is required when Bedrock is enabled")
	}

	return nil
}

// Settings represents user or team settings
type Settings struct {
	name      string
	bedrock   *BedrockSettings
	createdAt time.Time
	updatedAt time.Time
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

	return nil
}
