package entities

import (
	"fmt"
	"strings"
)

// AWSEntityType represents the type of AWS IAM entity
type AWSEntityType string

const (
	AWSEntityTypeUser AWSEntityType = "user"
	AWSEntityTypeRole AWSEntityType = "role"
)

// AWSUserInfo contains AWS IAM-specific user information
type AWSUserInfo struct {
	arn        string
	userID     string
	accountID  string
	entityType AWSEntityType
	entityName string
	tags       map[string]string
	teams      []string
}

// NewAWSUserInfo creates a new AWSUserInfo
func NewAWSUserInfo(arn, userID, accountID string, entityType AWSEntityType, entityName string, tags map[string]string, teams []string) *AWSUserInfo {
	tagsCopy := make(map[string]string, len(tags))
	for k, v := range tags {
		tagsCopy[k] = v
	}

	teamsCopy := make([]string, len(teams))
	copy(teamsCopy, teams)

	return &AWSUserInfo{
		arn:        arn,
		userID:     userID,
		accountID:  accountID,
		entityType: entityType,
		entityName: entityName,
		tags:       tagsCopy,
		teams:      teamsCopy,
	}
}

// ARN returns the AWS ARN
func (a *AWSUserInfo) ARN() string {
	return a.arn
}

// UserID returns the AWS unique user ID
func (a *AWSUserInfo) UserID() string {
	return a.userID
}

// AccountID returns the AWS account ID
func (a *AWSUserInfo) AccountID() string {
	return a.accountID
}

// EntityType returns the AWS entity type (user or role)
func (a *AWSUserInfo) EntityType() AWSEntityType {
	return a.entityType
}

// EntityName returns the entity name (username or role name)
func (a *AWSUserInfo) EntityName() string {
	return a.entityName
}

// Tags returns a copy of the IAM tags
func (a *AWSUserInfo) Tags() map[string]string {
	tagsCopy := make(map[string]string, len(a.tags))
	for k, v := range a.tags {
		tagsCopy[k] = v
	}
	return tagsCopy
}

// GetTag returns the value of a specific tag
func (a *AWSUserInfo) GetTag(key string) (string, bool) {
	value, ok := a.tags[key]
	return value, ok
}

// Teams returns a copy of the teams extracted from tags
func (a *AWSUserInfo) Teams() []string {
	teamsCopy := make([]string, len(a.teams))
	copy(teamsCopy, a.teams)
	return teamsCopy
}

// IsRole returns true if the entity is an IAM Role
func (a *AWSUserInfo) IsRole() bool {
	return a.entityType == AWSEntityTypeRole
}

// IsUser returns true if the entity is an IAM User
func (a *AWSUserInfo) IsUser() bool {
	return a.entityType == AWSEntityTypeUser
}

// ParseARN parses an AWS ARN and returns entity type and name
// ARN format: arn:aws:iam::123456789012:user/username
// ARN format: arn:aws:iam::123456789012:role/rolename
// ARN format: arn:aws:sts::123456789012:assumed-role/rolename/session-name
func ParseARN(arn string) (entityType AWSEntityType, entityName string, err error) {
	parts := strings.Split(arn, ":")
	if len(parts) < 6 {
		return "", "", fmt.Errorf("invalid ARN format: %s", arn)
	}

	resource := parts[5]

	if strings.HasPrefix(resource, "user/") {
		entityName = strings.TrimPrefix(resource, "user/")
		entityType = AWSEntityTypeUser
		return entityType, entityName, nil
	}

	if strings.HasPrefix(resource, "role/") {
		entityName = strings.TrimPrefix(resource, "role/")
		entityType = AWSEntityTypeRole
		return entityType, entityName, nil
	}

	if strings.HasPrefix(resource, "assumed-role/") {
		// Format: assumed-role/rolename/session-name
		resourceParts := strings.Split(strings.TrimPrefix(resource, "assumed-role/"), "/")
		if len(resourceParts) >= 1 {
			entityName = resourceParts[0]
			entityType = AWSEntityTypeRole
			return entityType, entityName, nil
		}
	}

	return "", "", fmt.Errorf("unsupported ARN resource type: %s", resource)
}

// ExtractAccountID extracts the AWS account ID from an ARN
func ExtractAccountID(arn string) (string, error) {
	parts := strings.Split(arn, ":")
	if len(parts) < 5 {
		return "", fmt.Errorf("invalid ARN format: %s", arn)
	}
	return parts[4], nil
}
