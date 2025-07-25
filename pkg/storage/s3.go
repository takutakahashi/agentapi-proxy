package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/takutakahashi/agentapi-proxy/pkg/utils"
)

// S3Storage implements Storage interface using AWS S3
type S3Storage struct {
	client         *s3.Client
	bucket         string
	prefix         string
	encryptSecrets bool
	mutex          sync.RWMutex
}

// NewS3Storage creates a new S3 storage instance
func NewS3Storage(bucket, region, prefix, endpoint, accessKey, secretKey string, encryptSecrets bool) (*S3Storage, error) {
	if bucket == "" {
		return nil, fmt.Errorf("S3 bucket name is required")
	}

	if region == "" {
		region = "us-east-1"
	}

	if prefix == "" {
		prefix = "sessions/"
	}

	// Ensure prefix ends with /
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	ctx := context.Background()

	// Create AWS config
	var cfg aws.Config
	var err error

	if accessKey != "" && secretKey != "" {
		// Use static credentials
		cfg, err = config.LoadDefaultConfig(ctx,
			config.WithRegion(region),
			config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
		)
	} else {
		// Use default credentials chain (IAM role, environment variables, etc.)
		cfg, err = config.LoadDefaultConfig(ctx,
			config.WithRegion(region),
		)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create S3 client
	var client *s3.Client
	if endpoint != "" {
		// Custom endpoint (for S3-compatible services)
		client = s3.NewFromConfig(cfg, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
			o.UsePathStyle = true
		})
	} else {
		client = s3.NewFromConfig(cfg)
	}

	// Test connection by checking if bucket exists
	_, err = client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to access S3 bucket '%s': %w", bucket, err)
	}

	log.Printf("S3 storage initialized: bucket=%s, region=%s, prefix=%s", bucket, region, prefix)

	return &S3Storage{
		client:         client,
		bucket:         bucket,
		prefix:         prefix,
		encryptSecrets: encryptSecrets,
	}, nil
}

// Save stores a session in S3
func (s *S3Storage) Save(session *SessionData) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Apply encryption if enabled
	sessionToSave := *session
	if s.encryptSecrets {
		if encrypted, err := encryptSessionSecrets(&sessionToSave); err != nil {
			log.Printf("Warning: Failed to encrypt session secrets: %v", err)
		} else {
			sessionToSave = *encrypted
		}
	}

	// Marshal session to JSON
	jsonStr, err := utils.MarshalJSONString(sessionToSave)
	if err != nil {
		return fmt.Errorf("failed to marshal session data: %w", err)
	}
	data := []byte(jsonStr)

	// Create S3 key
	key := s.prefix + session.ID + ".json"

	// Upload to S3
	_, err = s.client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return fmt.Errorf("failed to save session to S3: %w", err)
	}

	return nil
}

// Load retrieves a session from S3
func (s *S3Storage) Load(sessionID string) (*SessionData, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	// Create S3 key
	key := s.prefix + sessionID + ".json"

	// Download from S3
	result, err := s.client.GetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if strings.Contains(err.Error(), "NoSuchKey") || strings.Contains(err.Error(), "NotFound") {
			return nil, fmt.Errorf("session not found")
		}
		return nil, fmt.Errorf("failed to load session from S3: %w", err)
	}
	defer func() {
		if closeErr := result.Body.Close(); closeErr != nil {
			// Log the error or handle it appropriately
			_ = closeErr
		}
	}()

	// Read response body
	data, err := io.ReadAll(result.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read S3 response: %w", err)
	}

	// Unmarshal JSON
	var session SessionData
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session data: %w", err)
	}

	// Decrypt secrets if encryption is enabled
	if s.encryptSecrets {
		if decrypted, err := decryptSessionSecrets(&session); err != nil {
			log.Printf("Warning: Failed to decrypt session secrets: %v", err)
		} else {
			session = *decrypted
		}
	}

	return &session, nil
}

// LoadAll retrieves all sessions from S3
func (s *S3Storage) LoadAll() ([]*SessionData, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var sessions []*SessionData

	// List objects with prefix
	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(s.prefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(context.Background())
		if err != nil {
			return nil, fmt.Errorf("failed to list S3 objects: %w", err)
		}

		for _, obj := range page.Contents {
			if obj.Key == nil || !strings.HasSuffix(*obj.Key, ".json") {
				continue
			}

			// Extract session ID from key
			sessionID := strings.TrimSuffix(strings.TrimPrefix(*obj.Key, s.prefix), ".json")

			// Load session
			session, err := s.Load(sessionID)
			if err != nil {
				log.Printf("Warning: Failed to load session %s: %v", sessionID, err)
				continue
			}

			sessions = append(sessions, session)
		}
	}

	return sessions, nil
}

// Delete removes a session from S3
func (s *S3Storage) Delete(sessionID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Create S3 key
	key := s.prefix + sessionID + ".json"

	// Delete from S3
	_, err := s.client.DeleteObject(context.Background(), &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("failed to delete session from S3: %w", err)
	}

	return nil
}

// Update updates an existing session in S3
func (s *S3Storage) Update(session *SessionData) error {
	// For S3, update is the same as save
	return s.Save(session)
}

// Close cleans up resources
func (s *S3Storage) Close() error {
	// S3 client doesn't require explicit cleanup
	return nil
}
