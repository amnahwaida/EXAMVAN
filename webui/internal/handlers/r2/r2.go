package r2

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// ErrMsgNotConfigured is the canonical user-facing message every handler
// returns when the R2 backend is missing or disabled. Handlers must use this
// constant (not a local literal) so the wording stays identical across
// admin/api/public surfaces.
const ErrMsgNotConfigured = "Cloudflare R2 tidak dikonfigurasi."

// Client is the object-storage abstraction the handlers depend on. The
// concrete Cloudflare R2 implementation is S3Client; tests can substitute a
// stub (e.g. recording the keys passed to Delete) without a network call.
type Client interface {
	// Enabled reports whether the storage backend is configured and ready.
	Enabled() bool
	// Upload reads from reader and uploads with the given key (default PDF).
	Upload(ctx context.Context, key string, reader io.Reader) error
	// UploadWithContentType uploads with an explicit content type.
	UploadWithContentType(ctx context.Context, key string, reader io.Reader, contentType string) error
	// UploadBytes uploads raw bytes with the given key.
	UploadBytes(ctx context.Context, key string, data []byte) error
	// SignedURL generates a temporary download URL valid for the given duration.
	SignedURL(ctx context.Context, key string, ttl time.Duration) (string, error)
	// Delete removes a file with the given key.
	Delete(ctx context.Context, key string) error
}

// FromContext extracts the Client stored in a gin context value (the "r2"
// key), or nil when the value is missing or not a Client. Handlers use this
// instead of a direct type assertion so a nil/disabled backend is handled
// uniformly (Enabled() already guards a typed-nil receiver).
func FromContext(v interface{}) Client {
	cl, _ := v.(Client)
	return cl
}

// S3Client wraps an S3-compatible client for Cloudflare R2.
type S3Client struct {
	S3      *s3.Client
	Bucket  string
	enabled bool
}

// NewClient creates an R2 client. Returns nil if credentials are missing or
// the AWS config cannot be loaded. R2 is mandatory (config.Load() fails fast
// without credentials), so callers must treat a nil return as fatal.
func NewClient(accessKey, secretKey, endpoint, bucket string) *S3Client {
	if accessKey == "" || secretKey == "" || endpoint == "" {
		log.Println("r2: credentials missing — R2 disabled (mandatory)")
		return nil
	}

	cfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion("auto"),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		),
	)
	if err != nil {
		log.Printf("r2: init config failed: %v — R2 disabled", err)
		return nil
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	})

	log.Println("r2: connected — PDF will be served via Cloudflare R2")
	return &S3Client{
		S3:      client,
		Bucket:  bucket,
		enabled: true,
	}
}

// Enabled returns true if R2 is configured and ready.
func (c *S3Client) Enabled() bool {
	return c != nil && c.enabled
}

// Upload reads from reader and uploads to R2 with the given key (default to pdf).
func (c *S3Client) Upload(ctx context.Context, key string, reader io.Reader) error {
	return c.UploadWithContentType(ctx, key, reader, "application/pdf")
}

// UploadWithContentType reads from reader and uploads to R2 with the given key and content type.
func (c *S3Client) UploadWithContentType(ctx context.Context, key string, reader io.Reader, contentType string) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("r2 read: %w", err)
	}

	_, err = c.S3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.Bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("r2 upload: %w", err)
	}
	log.Printf("r2: uploaded %s (%d bytes)", key, len(data))
	return nil
}

// UploadBytes uploads raw bytes to R2 with the given key.
func (c *S3Client) UploadBytes(ctx context.Context, key string, data []byte) error {
	_, err := c.S3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.Bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String("application/pdf"),
	})
	if err != nil {
		return fmt.Errorf("r2 upload: %w", err)
	}
	log.Printf("r2: uploaded %s (%d bytes)", key, len(data))
	return nil
}

// SignedURL generates a temporary download URL valid for the given duration.
func (c *S3Client) SignedURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	presign := s3.NewPresignClient(c.S3)

	req, err := presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.Bucket),
		Key:    aws.String(key),
	}, func(po *s3.PresignOptions) {
		po.Expires = ttl
	})
	if err != nil {
		return "", fmt.Errorf("r2 presign: %w", err)
	}

	return req.URL, nil
}

// Delete removes a file from R2.
func (c *S3Client) Delete(ctx context.Context, key string) error {
	_, err := c.S3.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("r2 delete: %w", err)
	}
	log.Printf("r2: deleted %s", key)
	return nil
}
