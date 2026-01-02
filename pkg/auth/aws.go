package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/utils"
)

// AWSCredentials represents AWS credentials extracted from the request
type AWSCredentials struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}

// AWSUserCache represents cached AWS user information
type AWSUserCache struct {
	Info        *entities.AWSUserInfo
	Role        string
	Permissions []string
	EnvFile     string
}

// AWSAuthProvider handles AWS IAM authentication
type AWSAuthProvider struct {
	config    *config.AWSAuthConfig
	iamClient *iam.Client
	userCache *utils.TTLCache
}

// NewAWSAuthProvider creates a new AWS authentication provider
func NewAWSAuthProvider(cfg *config.AWSAuthConfig) (*AWSAuthProvider, error) {
	// Parse cache TTL
	cacheTTL := 1 * time.Hour
	if cfg.CacheTTL != "" {
		if parsed, err := time.ParseDuration(cfg.CacheTTL); err == nil {
			cacheTTL = parsed
		}
	}

	// Use very short cache TTL in tests
	if isTestEnvironment() {
		cacheTTL = 1 * time.Millisecond
	}

	// Load AWS config from environment/instance profile
	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(cfg.Region),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return &AWSAuthProvider{
		config:    cfg,
		iamClient: iam.NewFromConfig(awsCfg),
		userCache: utils.NewTTLCache(cacheTTL),
	}, nil
}

// Authenticate authenticates a user using AWS credentials from Basic Auth
// It verifies the user/role exists and has the required tag using proxy's IAM permissions
func (p *AWSAuthProvider) Authenticate(ctx context.Context, creds *AWSCredentials) (*UserContext, error) {
	// Check cache first
	cacheKey := p.cacheKey(creds.AccessKeyID)
	if !isTestEnvironment() {
		if cached, found := p.userCache.Get(cacheKey); found {
			cachedUser := cached.(*AWSUserCache)

			// Re-apply environment variables if specified
			if cachedUser.EnvFile != "" {
				envVars, err := config.LoadTeamEnvVars(cachedUser.EnvFile)
				if err != nil {
					log.Printf("[AWS_AUTH] Warning: Failed to load cached env file %s: %v", cachedUser.EnvFile, err)
				} else {
					applied := config.ApplyEnvVars(envVars)
					log.Printf("[AWS_AUTH] Applied %d environment variables from cached %s", len(applied), cachedUser.EnvFile)
				}
			}

			return &UserContext{
				UserID:      cachedUser.Info.EntityName(),
				Role:        cachedUser.Role,
				Permissions: cachedUser.Permissions,
				AuthType:    "aws",
				EnvFile:     cachedUser.EnvFile,
			}, nil
		}
	}

	// Look up user by access key ID using proxy's IAM permissions
	userInfo, tags, err := p.lookupUserByAccessKey(ctx, creds.AccessKeyID)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup user: %w", err)
	}

	// Verify account ID if configured
	if p.config.AccountID != "" && userInfo.AccountID() != p.config.AccountID {
		return nil, fmt.Errorf("account %s is not allowed (expected %s)", userInfo.AccountID(), p.config.AccountID)
	}

	// Check required tag
	if p.config.RequiredTagKey != "" {
		tagValue, hasTag := tags[p.config.RequiredTagKey]
		if !hasTag {
			return nil, fmt.Errorf("user/role %s does not have required tag %s", userInfo.EntityName(), p.config.RequiredTagKey)
		}
		if p.config.RequiredTagVal != "" && tagValue != p.config.RequiredTagVal {
			return nil, fmt.Errorf("user/role %s has tag %s=%s, expected %s", userInfo.EntityName(), p.config.RequiredTagKey, tagValue, p.config.RequiredTagVal)
		}
		log.Printf("[AWS_AUTH] User %s has required tag %s=%s", userInfo.EntityName(), p.config.RequiredTagKey, tagValue)
	}

	// Extract teams from tags
	teams := p.extractTeamsFromTags(tags)
	log.Printf("[AWS_AUTH] Extracted teams from tags: %v", teams)

	// Map permissions based on teams
	role, permissions, envFile := p.mapUserPermissions(teams)
	log.Printf("[AWS_AUTH] Mapped permissions: role=%s, permissions=%v, envFile=%s", role, permissions, envFile)

	// Load environment variables if specified
	if envFile != "" {
		envVars, err := config.LoadTeamEnvVars(envFile)
		if err != nil {
			log.Printf("[AWS_AUTH] Warning: Failed to load env file %s: %v", envFile, err)
		} else {
			applied := config.ApplyEnvVars(envVars)
			log.Printf("[AWS_AUTH] Applied %d environment variables from %s", len(applied), envFile)
		}
	}

	// Update userInfo with teams
	awsInfo := entities.NewAWSUserInfo(
		userInfo.ARN(),
		userInfo.UserID(),
		userInfo.AccountID(),
		userInfo.EntityType(),
		userInfo.EntityName(),
		tags,
		teams,
	)

	// Cache the user information
	if !isTestEnvironment() {
		p.userCache.Set(cacheKey, &AWSUserCache{
			Info:        awsInfo,
			Role:        role,
			Permissions: permissions,
			EnvFile:     envFile,
		})
		log.Printf("[AWS_AUTH] Cached user info for %s", awsInfo.EntityName())
	}

	return &UserContext{
		UserID:      awsInfo.EntityName(),
		Role:        role,
		Permissions: permissions,
		AuthType:    "aws",
		EnvFile:     envFile,
	}, nil
}

// lookupUserByAccessKey looks up a user by their access key ID using proxy's IAM permissions
func (p *AWSAuthProvider) lookupUserByAccessKey(ctx context.Context, accessKeyID string) (*entities.AWSUserInfo, map[string]string, error) {
	// Get access key last used info to find the user
	accessKeyOutput, err := p.iamClient.GetAccessKeyLastUsed(ctx, &iam.GetAccessKeyLastUsedInput{
		AccessKeyId: &accessKeyID,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("access key not found or invalid: %w", err)
	}

	userName := accessKeyOutput.UserName
	if userName == nil || *userName == "" {
		return nil, nil, fmt.Errorf("access key is not associated with a user (may be root account)")
	}

	log.Printf("[AWS_AUTH] Found user %s for access key %s...", *userName, accessKeyID[:8])

	// Get user details
	userOutput, err := p.iamClient.GetUser(ctx, &iam.GetUserInput{
		UserName: userName,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get user details: %w", err)
	}

	// Extract account ID from ARN
	accountID, err := entities.ExtractAccountID(*userOutput.User.Arn)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to extract account ID: %w", err)
	}

	// Get user tags
	tagsOutput, err := p.iamClient.ListUserTags(ctx, &iam.ListUserTagsInput{
		UserName: userName,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get user tags: %w", err)
	}

	tags := make(map[string]string)
	for _, tag := range tagsOutput.Tags {
		tags[*tag.Key] = *tag.Value
	}

	log.Printf("[AWS_AUTH] Retrieved tags for user %s: %v", *userName, tags)

	userInfo := entities.NewAWSUserInfo(
		*userOutput.User.Arn,
		*userOutput.User.UserId,
		accountID,
		entities.AWSEntityTypeUser,
		*userName,
		tags,
		nil, // teams will be extracted later
	)

	return userInfo, tags, nil
}

// extractTeamsFromTags extracts team names from IAM tags
func (p *AWSAuthProvider) extractTeamsFromTags(tags map[string]string) []string {
	teamTagKey := p.config.TeamTagKey
	if teamTagKey == "" {
		teamTagKey = "Team"
	}

	teamValue, ok := tags[teamTagKey]
	if !ok {
		return nil
	}

	// Support comma-separated teams
	teams := strings.Split(teamValue, ",")
	result := make([]string, 0, len(teams))
	for _, team := range teams {
		team = strings.TrimSpace(team)
		if team != "" {
			result = append(result, team)
		}
	}
	return result
}

// mapUserPermissions maps teams to roles and permissions
func (p *AWSAuthProvider) mapUserPermissions(teams []string) (string, []string, string) {
	role := p.config.UserMapping.DefaultRole
	if role == "" {
		role = "user"
	}

	allPermissions := make(map[string]bool)
	envFile := ""

	// Add default permissions
	for _, perm := range p.config.UserMapping.DefaultPermissions {
		allPermissions[perm] = true
	}

	// Check each team against configured rules
	for _, team := range teams {
		if rule, exists := p.config.UserMapping.TeamRoleMapping[team]; exists {
			log.Printf("[AWS_AUTH] Found matching rule for team %s: role=%s, permissions=%v", team, rule.Role, rule.Permissions)

			// Apply higher role if found
			if isHigherRole(rule.Role, role) {
				role = rule.Role
				if rule.EnvFile != "" {
					envFile = rule.EnvFile
				}
			}

			// Add permissions from this rule
			for _, perm := range rule.Permissions {
				allPermissions[perm] = true
			}
		}
	}

	// Convert permissions map to slice
	permissions := make([]string, 0, len(allPermissions))
	for perm := range allPermissions {
		permissions = append(permissions, perm)
	}

	return role, permissions, envFile
}

// cacheKey generates a cache key from the access key ID
func (p *AWSAuthProvider) cacheKey(accessKeyID string) string {
	h := sha256.Sum256([]byte(accessKeyID))
	return fmt.Sprintf("aws:%s", hex.EncodeToString(h[:]))
}

// isHigherRole checks if role1 has higher priority than role2
func isHigherRole(role1, role2 string) bool {
	rolePriority := map[string]int{
		"guest":     0,
		"user":      1,
		"member":    2,
		"developer": 3,
		"admin":     4,
	}

	priority1, exists1 := rolePriority[role1]
	priority2, exists2 := rolePriority[role2]

	if !exists1 || !exists2 {
		return false
	}

	return priority1 > priority2
}

// ExtractAWSCredentialsFromBasicAuth extracts AWS credentials from Basic Auth header
func ExtractAWSCredentialsFromBasicAuth(r *http.Request) (*AWSCredentials, bool) {
	username, password, ok := r.BasicAuth()
	if !ok {
		return nil, false
	}

	// Check if username looks like an AWS Access Key ID
	// AWS Access Key IDs start with AKIA (permanent) or ASIA (temporary)
	if !IsAWSAccessKeyID(username) {
		return nil, false
	}

	return &AWSCredentials{
		AccessKeyID:     username,
		SecretAccessKey: password,
		SessionToken:    r.Header.Get("X-AWS-Session-Token"),
	}, true
}

// IsAWSAccessKeyID checks if the string is a valid AWS Access Key ID format
func IsAWSAccessKeyID(s string) bool {
	if len(s) != 20 {
		return false
	}
	return strings.HasPrefix(s, "AKIA") || strings.HasPrefix(s, "ASIA")
}
