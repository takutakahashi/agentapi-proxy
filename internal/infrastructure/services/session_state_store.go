package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

// SessionStateStore is the backend-owned durable store for ACP snapshots.
type SessionStateStore interface {
	Save(context.Context, string, io.Reader) error
	Load(context.Context, string) (io.ReadCloser, error)
}

type MultipartPart struct {
	Number int32  `json:"number"`
	ETag   string `json:"etag"`
}

type MultipartSessionStateStore interface {
	SessionStateStore
	BeginMultipart(context.Context, string) (string, int64, error)
	PresignPart(context.Context, string, string, int32) (string, error)
	CompleteMultipart(context.Context, string, string, []MultipartPart) error
	AbortMultipart(context.Context, string, string) error
	PresignDownload(context.Context, string) (string, error)
}

type volumeSessionStateStore struct{ root string }

func newVolumeSessionStateStore(root string) (SessionStateStore, error) {
	if root == "" {
		return nil, fmt.Errorf("session persistence path is required")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	return &volumeSessionStateStore{root: root}, nil
}

func safeSessionStateID(id string) error {
	if id == "" || filepath.Base(id) != id || strings.ContainsAny(id, `/\\`) {
		return fmt.Errorf("invalid session state id")
	}
	return nil
}

func (s *volumeSessionStateStore) path(id string) (string, error) {
	if err := safeSessionStateID(id); err != nil {
		return "", err
	}
	return filepath.Join(s.root, id+".tar.gz"), nil
}
func (s *volumeSessionStateStore) Save(_ context.Context, id string, data io.Reader) error {
	p, err := s.path(id)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.root, id+"-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err = io.Copy(tmp, data); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, p)
}
func (s *volumeSessionStateStore) Load(_ context.Context, id string) (io.ReadCloser, error) {
	p, err := s.path(id)
	if err != nil {
		return nil, err
	}
	return os.Open(p)
}

type sessionStateS3Client interface {
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	CreateMultipartUpload(context.Context, *s3.CreateMultipartUploadInput, ...func(*s3.Options)) (*s3.CreateMultipartUploadOutput, error)
	CompleteMultipartUpload(context.Context, *s3.CompleteMultipartUploadInput, ...func(*s3.Options)) (*s3.CompleteMultipartUploadOutput, error)
	AbortMultipartUpload(context.Context, *s3.AbortMultipartUploadInput, ...func(*s3.Options)) (*s3.AbortMultipartUploadOutput, error)
}
type sessionStateS3Uploader interface {
	Upload(context.Context, *s3.PutObjectInput, ...func(*manager.Uploader)) (*manager.UploadOutput, error)
}
type s3SessionStateStore struct {
	client         sessionStateS3Client
	presigner      *s3.PresignClient
	uploader       sessionStateS3Uploader
	bucket, prefix string
}

func newS3SessionStateStore(ctx context.Context, cfg *config.MemoryS3Config) (SessionStateStore, error) {
	if cfg == nil || cfg.Bucket == "" {
		return nil, fmt.Errorf("session persistence S3 bucket is required")
	}
	opts := []func(*awsconfig.LoadOptions) error{}
	if cfg.Region != "" {
		opts = append(opts, awsconfig.WithRegion(cfg.Region))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, err
	}
	s3opts := []func(*s3.Options){}
	if cfg.Endpoint != "" {
		s3opts = append(s3opts, func(o *s3.Options) { o.BaseEndpoint = aws.String(cfg.Endpoint); o.UsePathStyle = true })
	}
	prefix := cfg.Prefix
	if prefix == "" {
		prefix = "agentapi-sessions/"
	}
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	client := s3.NewFromConfig(awsCfg, s3opts...)
	uploader := manager.NewUploader(client, func(u *manager.Uploader) {
		u.PartSize = 8 << 20
		u.Concurrency = 2
	})
	return &s3SessionStateStore{client: client, presigner: s3.NewPresignClient(client), uploader: uploader, bucket: cfg.Bucket, prefix: prefix}, nil
}
func (s *s3SessionStateStore) key(id string) (string, error) {
	if err := safeSessionStateID(id); err != nil {
		return "", err
	}
	return s.prefix + id + ".tar.gz", nil
}
func (s *s3SessionStateStore) Save(ctx context.Context, id string, data io.Reader) error {
	k, err := s.key(id)
	if err != nil {
		return err
	}
	_, err = s.uploader.Upload(ctx, &s3.PutObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(k), Body: data, ContentType: aws.String("application/zstd")})
	return err
}

const directUploadPartSize = 8 << 20

func (s *s3SessionStateStore) BeginMultipart(ctx context.Context, id string) (string, int64, error) {
	k, err := s.key(id)
	if err != nil {
		return "", 0, err
	}
	out, err := s.client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{Bucket: aws.String(s.bucket), Key: aws.String(k), ContentType: aws.String("application/zstd")})
	if err != nil {
		return "", 0, err
	}
	return aws.ToString(out.UploadId), directUploadPartSize, nil
}

func (s *s3SessionStateStore) PresignPart(ctx context.Context, id, uploadID string, number int32) (string, error) {
	k, err := s.key(id)
	if err != nil {
		return "", err
	}
	out, err := s.presigner.PresignUploadPart(ctx, &s3.UploadPartInput{Bucket: aws.String(s.bucket), Key: aws.String(k), UploadId: aws.String(uploadID), PartNumber: aws.Int32(number)}, s3.WithPresignExpires(10*time.Minute))
	if err != nil {
		return "", err
	}
	return out.URL, nil
}

func (s *s3SessionStateStore) CompleteMultipart(ctx context.Context, id, uploadID string, parts []MultipartPart) error {
	k, err := s.key(id)
	if err != nil {
		return err
	}
	completed := make([]types.CompletedPart, 0, len(parts))
	for _, part := range parts {
		completed = append(completed, types.CompletedPart{PartNumber: aws.Int32(part.Number), ETag: aws.String(part.ETag)})
	}
	_, err = s.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{Bucket: aws.String(s.bucket), Key: aws.String(k), UploadId: aws.String(uploadID), MultipartUpload: &types.CompletedMultipartUpload{Parts: completed}})
	return err
}

func (s *s3SessionStateStore) AbortMultipart(ctx context.Context, id, uploadID string) error {
	k, err := s.key(id)
	if err != nil {
		return err
	}
	_, err = s.client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{Bucket: aws.String(s.bucket), Key: aws.String(k), UploadId: aws.String(uploadID)})
	return err
}

func (s *s3SessionStateStore) PresignDownload(ctx context.Context, id string) (string, error) {
	k, err := s.key(id)
	if err != nil {
		return "", err
	}
	out, err := s.presigner.PresignGetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(k)}, s3.WithPresignExpires(10*time.Minute))
	if err != nil {
		return "", err
	}
	return out.URL, nil
}
func (s *s3SessionStateStore) Load(ctx context.Context, id string) (io.ReadCloser, error) {
	k, err := s.key(id)
	if err != nil {
		return nil, err
	}
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(k)})
	if err != nil {
		var nsk *types.NoSuchKey
		if errors.As(err, &nsk) {
			return nil, os.ErrNotExist
		}
		return nil, err
	}
	return out.Body, nil
}

// NewSessionStateStore builds the configured backend. Empty backend disables persistence.
func NewSessionStateStore(ctx context.Context, cfg config.SessionPersistenceConfig) (SessionStateStore, error) {
	switch cfg.Backend {
	case "":
		return nil, nil
	case "volume":
		return newVolumeSessionStateStore(cfg.Path)
	case "s3":
		return newS3SessionStateStore(ctx, cfg.S3)
	default:
		return nil, fmt.Errorf("unsupported session persistence backend %q", cfg.Backend)
	}
}
