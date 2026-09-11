package fileservice

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type minioAPI interface {
	putObject(context.Context, string, string, io.Reader, int64, minio.PutObjectOptions) (minio.UploadInfo, error)
	statObject(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error)
	getObject(context.Context, string, string, minio.GetObjectOptions) (io.ReadCloser, minio.ObjectInfo, http.Header, error)
	removeObject(context.Context, string, string, minio.RemoveObjectOptions) error
}

type minioClient struct{ client *minio.Client }

func (c minioClient) putObject(ctx context.Context, bucket, key string, body io.Reader, size int64, opts minio.PutObjectOptions) (minio.UploadInfo, error) {
	return c.client.PutObject(ctx, bucket, key, body, size, opts)
}
func (c minioClient) statObject(ctx context.Context, bucket, key string, opts minio.StatObjectOptions) (minio.ObjectInfo, error) {
	return c.client.StatObject(ctx, bucket, key, opts)
}
func (c minioClient) getObject(ctx context.Context, bucket, key string, opts minio.GetObjectOptions) (io.ReadCloser, minio.ObjectInfo, http.Header, error) {
	object, err := c.client.GetObject(ctx, bucket, key, opts)
	if err != nil {
		return nil, minio.ObjectInfo{}, nil, err
	}
	// GetObject is lazy. Stat forces an authenticated round trip here so the
	// HTTP layer can still return 503 instead of committing a false 206.
	info, err := object.Stat()
	if err != nil {
		_ = object.Close()
		return nil, minio.ObjectInfo{}, nil, err
	}
	return object, info, nil, nil
}
func (c minioClient) removeObject(ctx context.Context, bucket, key string, opts minio.RemoveObjectOptions) error {
	return c.client.RemoveObject(ctx, bucket, key, opts)
}

type MinIOObjectStore struct {
	client minioAPI
	bucket string
}

func NewMinIOObjectStore(endpoint, accessKey, secretKey, bucket string, secure bool) (*MinIOObjectStore, error) {
	if endpoint == "" || accessKey == "" || secretKey == "" || bucket == "" {
		return nil, fmt.Errorf("minio endpoint, credentials, and bucket are required")
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: secure,
	})
	if err != nil {
		return nil, fmt.Errorf("create minio client: %w", err)
	}
	return &MinIOObjectStore{client: minioClient{client: client}, bucket: bucket}, nil
}

func (s *MinIOObjectStore) Put(ctx context.Context, key string, body io.Reader, size int64, mediaType string) error {
	if s == nil || s.client == nil || s.bucket == "" || key == "" || body == nil || size < 0 {
		return fmt.Errorf("invalid minio put")
	}
	_, err := s.client.putObject(ctx, s.bucket, key, body, size, minio.PutObjectOptions{
		ContentType:      mediaType,
		DisableMultipart: true,
	})
	if err != nil {
		return fmt.Errorf("minio put object: %w", err)
	}
	return nil
}

func (s *MinIOObjectStore) Stat(ctx context.Context, key string) (ObjectInfo, error) {
	if s == nil || s.client == nil || s.bucket == "" || key == "" {
		return ObjectInfo{}, fmt.Errorf("invalid minio stat")
	}
	info, err := s.client.statObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("minio stat object: %w", err)
	}
	return ObjectInfo{Size: info.Size, ContentType: info.ContentType}, nil
}

func (s *MinIOObjectStore) Open(ctx context.Context, key string, offset, length int64) (io.ReadCloser, error) {
	if s == nil || s.client == nil || s.bucket == "" || key == "" || offset < 0 || length <= 0 {
		return nil, fmt.Errorf("invalid minio range")
	}
	opts := minio.GetObjectOptions{}
	if err := opts.SetRange(offset, offset+length-1); err != nil {
		return nil, fmt.Errorf("set minio range: %w", err)
	}
	body, _, _, err := s.client.getObject(ctx, s.bucket, key, opts)
	if err != nil {
		return nil, fmt.Errorf("minio get object: %w", err)
	}
	return body, nil
}

func (s *MinIOObjectStore) Delete(ctx context.Context, key string) error {
	if s == nil || s.client == nil || s.bucket == "" || key == "" {
		return fmt.Errorf("invalid minio delete")
	}
	if err := s.client.removeObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("minio remove object: %w", err)
	}
	return nil
}
