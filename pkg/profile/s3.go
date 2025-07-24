package profile

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Storage implements profile storage using AWS S3
type S3Storage struct {
	client *s3.Client
	bucket string
	prefix string
}

// NewS3Storage creates a new S3-based profile storage
func NewS3Storage(bucket, region, endpoint, prefix string) (*S3Storage, error) {
	if bucket == "" {
		return nil, fmt.Errorf("S3 bucket name is required")
	}

	// Load AWS config
	opts := []func(*config.LoadOptions) error{
		config.WithRegion(region),
	}

	// Use custom endpoint if provided (for S3-compatible services)
	if endpoint != "" {
		opts = append(opts, config.WithBaseEndpoint(endpoint))
	}

	cfg, err := config.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if endpoint != "" {
			o.UsePathStyle = true // Required for MinIO and some S3-compatible services
		}
	})

	return &S3Storage{
		client: client,
		bucket: bucket,
		prefix: strings.TrimSuffix(prefix, "/"),
	}, nil
}

// getObjectKey returns the S3 object key for a user's profile
func (s *S3Storage) getObjectKey(userID string) string {
	// Sanitize userID to prevent path traversal
	safeUserID := strings.ReplaceAll(userID, "..", "")
	safeUserID = strings.ReplaceAll(safeUserID, "/", "_")
	safeUserID = strings.ReplaceAll(safeUserID, "\\", "_")

	if s.prefix != "" {
		return fmt.Sprintf("%s/%s/profile.json", s.prefix, safeUserID)
	}
	return fmt.Sprintf("%s/profile.json", safeUserID)
}

// Save stores a profile to S3
func (s *S3Storage) Save(ctx context.Context, profile *Profile) error {
	if profile == nil || profile.UserID == "" {
		return ErrInvalidProfile
	}

	data, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal profile: %w", err)
	}

	key := s.getObjectKey(profile.UserID)

	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String("application/json"),
	})

	if err != nil {
		return fmt.Errorf("failed to save profile to S3: %w", err)
	}

	return nil
}

// Load retrieves a profile from S3
func (s *S3Storage) Load(ctx context.Context, userID string) (*Profile, error) {
	if userID == "" {
		return nil, ErrInvalidProfile
	}

	key := s.getObjectKey(userID)

	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})

	if err != nil {
		if strings.Contains(err.Error(), "NoSuchKey") || strings.Contains(err.Error(), "NotFound") {
			return nil, ErrProfileNotFound
		}
		return nil, fmt.Errorf("failed to get profile from S3: %w", err)
	}
	defer func() { _ = result.Body.Close() }()

	var profile Profile
	decoder := json.NewDecoder(result.Body)
	if err := decoder.Decode(&profile); err != nil {
		return nil, fmt.Errorf("failed to unmarshal profile: %w", err)
	}

	return &profile, nil
}

// Update updates an existing profile in S3
func (s *S3Storage) Update(ctx context.Context, userID string, update *ProfileUpdate) error {
	if userID == "" || update == nil {
		return ErrInvalidProfile
	}

	// Load existing profile
	profile, err := s.Load(ctx, userID)
	if err != nil {
		return err
	}

	// Apply updates
	profile.Update(update)

	// Save updated profile
	return s.Save(ctx, profile)
}

// Delete removes a profile from S3
func (s *S3Storage) Delete(ctx context.Context, userID string) error {
	if userID == "" {
		return ErrInvalidProfile
	}

	key := s.getObjectKey(userID)

	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})

	if err != nil {
		return fmt.Errorf("failed to delete profile from S3: %w", err)
	}

	return nil
}

// Exists checks if a profile exists in S3
func (s *S3Storage) Exists(ctx context.Context, userID string) (bool, error) {
	if userID == "" {
		return false, ErrInvalidProfile
	}

	key := s.getObjectKey(userID)

	_, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})

	if err != nil {
		if strings.Contains(err.Error(), "NoSuchKey") || strings.Contains(err.Error(), "NotFound") {
			return false, nil
		}
		return false, fmt.Errorf("failed to check profile existence: %w", err)
	}

	return true, nil
}

// List returns all profile IDs in S3
func (s *S3Storage) List(ctx context.Context) ([]string, error) {
	var userIDs []string

	prefix := s.prefix
	if prefix != "" {
		prefix += "/"
	}

	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			if strings.Contains(err.Error(), "NoSuchBucket") {
				return []string{}, nil
			}
			return nil, fmt.Errorf("failed to list profiles: %w", err)
		}

		for _, obj := range page.Contents {
			key := aws.ToString(obj.Key)
			// Extract user ID from key
			// Format: [prefix/]userID/profile.json
			parts := strings.Split(key, "/")
			if len(parts) >= 2 && parts[len(parts)-1] == "profile.json" {
				userID := parts[len(parts)-2]
				userIDs = append(userIDs, userID)
			}
		}
	}

	return userIDs, nil
}
