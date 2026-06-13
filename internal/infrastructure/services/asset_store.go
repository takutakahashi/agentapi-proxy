package services

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

// Asset is a stored static asset.
type Asset struct {
	ID  string
	URL string
}

// AssetStore stores HTML assets and returns externally reachable URLs.
type AssetStore interface {
	SaveHTML(ctx context.Context, userID string, html string) (*Asset, error)
}

// NewAssetStore creates an asset store from configuration.
func NewAssetStore(ctx context.Context, cfg config.AssetConfig) (AssetStore, error) {
	switch cfg.Backend {
	case "", "nginx":
		return NewFilesystemAssetStore(cfg.StoragePath, cfg.PublicBaseURL), nil
	case "s3":
		if cfg.S3 == nil || cfg.S3.Bucket == "" {
			return nil, fmt.Errorf("asset backend is s3 but asset.s3.bucket is empty")
		}
		return NewS3AssetStore(ctx, cfg)
	default:
		return nil, fmt.Errorf("unsupported asset backend %q", cfg.Backend)
	}
}

// FilesystemAssetStore writes HTML assets to a directory served by nginx.
type FilesystemAssetStore struct {
	root          string
	publicBaseURL string
}

// NewFilesystemAssetStore creates a filesystem-backed asset store.
func NewFilesystemAssetStore(root, publicBaseURL string) *FilesystemAssetStore {
	if root == "" {
		root = "/var/lib/agentapi-assets"
	}
	return &FilesystemAssetStore{
		root:          root,
		publicBaseURL: strings.TrimRight(publicBaseURL, "/"),
	}
}

// SaveHTML stores HTML as assets/<uuid>/index.html.
func (s *FilesystemAssetStore) SaveHTML(ctx context.Context, userID string, html string) (*Asset, error) {
	_ = ctx
	_ = userID

	id := uuid.New().String()
	key := assetKey(id)
	path := filepath.Join(s.root, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(html), 0o644); err != nil {
		return nil, err
	}
	return &Asset{ID: id, URL: joinURL(s.publicBaseURL, key)}, nil
}

// S3AssetStore writes HTML assets to S3 or S3-compatible storage.
type S3AssetStore struct {
	client        *s3.Client
	bucket        string
	prefix        string
	publicBaseURL string
}

// NewS3AssetStore creates an S3-backed asset store.
func NewS3AssetStore(ctx context.Context, cfg config.AssetConfig) (*S3AssetStore, error) {
	loadOpts := []func(*awsconfig.LoadOptions) error{}
	if cfg.S3.Region != "" {
		loadOpts = append(loadOpts, awsconfig.WithRegion(cfg.S3.Region))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.S3.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.S3.Endpoint)
			o.UsePathStyle = true
		}
	})
	publicBaseURL := strings.TrimRight(cfg.PublicBaseURL, "/")
	if publicBaseURL == "" {
		if cfg.S3.Endpoint != "" {
			publicBaseURL = strings.TrimRight(cfg.S3.Endpoint, "/") + "/" + cfg.S3.Bucket
		} else if cfg.S3.Region != "" {
			publicBaseURL = fmt.Sprintf("https://%s.s3.%s.amazonaws.com", cfg.S3.Bucket, cfg.S3.Region)
		} else {
			publicBaseURL = fmt.Sprintf("https://%s.s3.amazonaws.com", cfg.S3.Bucket)
		}
	}

	return &S3AssetStore{
		client:        client,
		bucket:        cfg.S3.Bucket,
		prefix:        strings.Trim(cfg.S3.Prefix, "/"),
		publicBaseURL: publicBaseURL,
	}, nil
}

// SaveHTML uploads HTML as assets/<uuid>/index.html.
func (s *S3AssetStore) SaveHTML(ctx context.Context, userID string, html string) (*Asset, error) {
	_ = userID

	id := uuid.New().String()
	key := assetKey(id)
	if s.prefix != "" {
		key = s.prefix + "/" + key
	}
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        strings.NewReader(html),
		ContentType: aws.String("text/html; charset=utf-8"),
	})
	if err != nil {
		return nil, err
	}
	return &Asset{ID: id, URL: joinURL(s.publicBaseURL, key)}, nil
}

func assetKey(id string) string {
	return "assets/" + id + "/index.html"
}

func joinURL(baseURL, key string) string {
	if baseURL == "" {
		return "/" + key
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return strings.TrimRight(baseURL, "/") + "/" + key
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/" + strings.TrimLeft(key, "/")
	return u.String()
}
