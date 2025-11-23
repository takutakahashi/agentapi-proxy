package storage

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// TestS3Storage tests the S3 storage implementation
func TestS3Storage(t *testing.T) {
	// Skip S3 integration tests (MinIO dependency removed)
	t.Skip("Skipping S3 integration test (MinIO dependency removed)")

	// Skip if not running integration tests
	if os.Getenv("INTEGRATION_TEST") != "true" {
		t.Skip("Skipping S3 integration test (set INTEGRATION_TEST=true to run)")
	}

	// MinIO connection details (should match GitHub Actions service container)
	bucket := "test-bucket"
	region := "us-east-1"
	prefix := "test-sessions/"
	endpoint := os.Getenv("MINIO_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://localhost:9000"
	}
	accessKey := os.Getenv("MINIO_ACCESS_KEY")
	if accessKey == "" {
		accessKey = "minioadmin"
	}
	secretKey := os.Getenv("MINIO_SECRET_KEY")
	if secretKey == "" {
		secretKey = "minioadmin"
	}

	// Create S3 storage
	storage, err := NewS3Storage(bucket, region, prefix, endpoint, accessKey, secretKey, false)
	if err != nil {
		// Try to create bucket if it doesn't exist
		storage = createTestBucket(t, bucket, region, endpoint, accessKey, secretKey)
		if storage == nil {
			t.Fatalf("Failed to create S3 storage: %v", err)
		}
	}
	defer func() { _ = storage.Close() }()

	// Run common storage interface tests
	testStorageInterface(t, storage)
}

// TestS3StorageWithEncryption tests S3 storage with encryption enabled
func TestS3StorageWithEncryption(t *testing.T) {
	// Skip S3 integration tests (MinIO dependency removed)
	t.Skip("Skipping S3 encryption integration test (MinIO dependency removed)")

	// Skip if not running integration tests
	if os.Getenv("INTEGRATION_TEST") != "true" {
		t.Skip("Skipping S3 encryption integration test (set INTEGRATION_TEST=true to run)")
	}

	// MinIO connection details
	bucket := "test-bucket-encrypted"
	region := "us-east-1"
	prefix := "encrypted-sessions/"
	endpoint := os.Getenv("MINIO_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://localhost:9000"
	}
	accessKey := os.Getenv("MINIO_ACCESS_KEY")
	if accessKey == "" {
		accessKey = "minioadmin"
	}
	secretKey := os.Getenv("MINIO_SECRET_KEY")
	if secretKey == "" {
		secretKey = "minioadmin"
	}

	// Create S3 storage with encryption
	storage, err := NewS3Storage(bucket, region, prefix, endpoint, accessKey, secretKey, true)
	if err != nil {
		// Try to create bucket if it doesn't exist
		storage = createTestBucket(t, bucket, region, endpoint, accessKey, secretKey)
		if storage == nil {
			t.Fatalf("Failed to create S3 storage with encryption: %v", err)
		}
	}
	defer func() { _ = storage.Close() }()

	// Test with sensitive environment variables
	sessionData := &SessionData{
		ID:        "test-s3-encrypted",
		Port:      9001,
		StartedAt: time.Now(),
		UserID:    "test-user",
		Status:    "active",
		Environment: map[string]string{
			"GITHUB_TOKEN": "sensitive-s3-token-123",
			"API_KEY":      "secret-s3-key-456",
			"NORMAL_VAR":   "not-sensitive",
		},
		Tags: map[string]string{
			"environment": "test",
		},
	}

	// Save session
	err = storage.Save(sessionData)
	if err != nil {
		t.Fatalf("Failed to save session: %v", err)
	}

	// Load session
	loaded, err := storage.Load(sessionData.ID)
	if err != nil {
		t.Fatalf("Failed to load session: %v", err)
	}

	// Check that sensitive data is properly decrypted
	if loaded.Environment["GITHUB_TOKEN"] != "sensitive-s3-token-123" {
		t.Errorf("Expected GITHUB_TOKEN to be decrypted, got: %s", loaded.Environment["GITHUB_TOKEN"])
	}
	if loaded.Environment["API_KEY"] != "secret-s3-key-456" {
		t.Errorf("Expected API_KEY to be decrypted, got: %s", loaded.Environment["API_KEY"])
	}
	if loaded.Environment["NORMAL_VAR"] != "not-sensitive" {
		t.Errorf("Expected NORMAL_VAR to remain unchanged, got: %s", loaded.Environment["NORMAL_VAR"])
	}
}

// TestS3StorageLoadAll tests loading all sessions from S3
func TestS3StorageLoadAll(t *testing.T) {
	// Skip S3 integration tests (MinIO dependency removed)
	t.Skip("Skipping S3 LoadAll integration test (MinIO dependency removed)")

	// Skip if not running integration tests
	if os.Getenv("INTEGRATION_TEST") != "true" {
		t.Skip("Skipping S3 LoadAll integration test (set INTEGRATION_TEST=true to run)")
	}

	// MinIO connection details
	bucket := "test-bucket-loadall"
	region := "us-east-1"
	prefix := "loadall-sessions/"
	endpoint := os.Getenv("MINIO_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://localhost:9000"
	}
	accessKey := os.Getenv("MINIO_ACCESS_KEY")
	if accessKey == "" {
		accessKey = "minioadmin"
	}
	secretKey := os.Getenv("MINIO_SECRET_KEY")
	if secretKey == "" {
		secretKey = "minioadmin"
	}

	// Create S3 storage
	storage, err := NewS3Storage(bucket, region, prefix, endpoint, accessKey, secretKey, false)
	if err != nil {
		// Try to create bucket if it doesn't exist
		storage = createTestBucket(t, bucket, region, endpoint, accessKey, secretKey)
		if storage == nil {
			t.Fatalf("Failed to create S3 storage: %v", err)
		}
	}
	defer func() { _ = storage.Close() }()

	// Create multiple sessions
	sessions := []*SessionData{
		{
			ID:        "loadall-session-1",
			Port:      9001,
			StartedAt: time.Now(),
			UserID:    "user1",
			Status:    "active",
			Environment: map[string]string{
				"VAR1": "value1",
			},
		},
		{
			ID:        "loadall-session-2",
			Port:      9002,
			StartedAt: time.Now(),
			UserID:    "user2",
			Status:    "inactive",
			Environment: map[string]string{
				"VAR2": "value2",
			},
		},
		{
			ID:        "loadall-session-3",
			Port:      9003,
			StartedAt: time.Now(),
			UserID:    "user3",
			Status:    "active",
			Environment: map[string]string{
				"VAR3": "value3",
			},
		},
	}

	// Save all sessions
	for _, session := range sessions {
		err := storage.Save(session)
		if err != nil {
			t.Fatalf("Failed to save session %s: %v", session.ID, err)
		}
	}

	// Load all sessions
	loadedSessions, err := storage.LoadAll()
	if err != nil {
		t.Fatalf("Failed to load all sessions: %v", err)
	}

	// Verify all sessions were loaded
	if len(loadedSessions) != len(sessions) {
		t.Errorf("Expected %d sessions, got %d", len(sessions), len(loadedSessions))
	}

	// Check each session exists
	sessionMap := make(map[string]*SessionData)
	for _, session := range loadedSessions {
		sessionMap[session.ID] = session
	}

	for _, expectedSession := range sessions {
		loadedSession, exists := sessionMap[expectedSession.ID]
		if !exists {
			t.Errorf("Session %s not found in loaded sessions", expectedSession.ID)
			continue
		}

		// Verify session data
		if loadedSession.Port != expectedSession.Port {
			t.Errorf("Session %s: expected port %d, got %d", expectedSession.ID, expectedSession.Port, loadedSession.Port)
		}
		if loadedSession.UserID != expectedSession.UserID {
			t.Errorf("Session %s: expected user %s, got %s", expectedSession.ID, expectedSession.UserID, loadedSession.UserID)
		}
		if loadedSession.Status != expectedSession.Status {
			t.Errorf("Session %s: expected status %s, got %s", expectedSession.ID, expectedSession.Status, loadedSession.Status)
		}
	}

	// Clean up
	for _, session := range sessions {
		_ = storage.Delete(session.ID)
	}
}

// createTestBucket creates a test bucket in MinIO
func createTestBucket(t *testing.T, bucket, region, endpoint, accessKey, secretKey string) *S3Storage {
	// First create the bucket
	storage := &S3Storage{
		bucket:         bucket,
		prefix:         "test-sessions/",
		encryptSecrets: false,
	}

	// Create S3 client configuration
	cfg := aws.Config{
		Region: region,
		Credentials: aws.CredentialsProviderFunc(func(ctx context.Context) (aws.Credentials, error) {
			return aws.Credentials{
				AccessKeyID:     accessKey,
				SecretAccessKey: secretKey,
			}, nil
		}),
	}

	// Create S3 client with custom endpoint
	storage.client = s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	})

	// Create bucket
	_, err := storage.client.CreateBucket(context.Background(), &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		// Bucket might already exist, which is fine
		t.Logf("Note: Bucket creation returned error (might already exist): %v", err)
	}

	// Verify we can access the bucket
	_, err = storage.client.HeadBucket(context.Background(), &s3.HeadBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		t.Fatalf("Failed to access bucket %s: %v", bucket, err)
	}

	return storage
}

// TestS3StorageInvalidConfig tests S3 storage with invalid configurations
func TestS3StorageInvalidConfig(t *testing.T) {
	// Test missing bucket name
	_, err := NewS3Storage("", "us-east-1", "prefix/", "", "", "", false)
	if err == nil {
		t.Error("Expected error for missing bucket name, got nil")
	}

	// Test invalid endpoint (should fail on connection test)
	_, err = NewS3Storage("test-bucket", "us-east-1", "prefix/", "http://invalid-endpoint:9999", "access", "secret", false)
	if err == nil {
		t.Error("Expected error for invalid endpoint, got nil")
	}
}
