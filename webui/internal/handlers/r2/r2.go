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
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const (
	// r2PartSize is the multipart chunk size for large uploads (installer
	// files can be ~100 MB). R2 requires parts of at least 5 MiB.
	r2PartSize = 16 * 1024 * 1024
	// r2UploadConcurrency uploads several parts in parallel. R2 is stricter
	// than S3 about concurrent parts, so keep this conservative.
	r2UploadConcurrency = 3
)

// ErrMsgNotConfigured is the canonical user-facing message every handler
// returns when the R2 backend is missing or disabled. Handlers must use this
// constant (not a local literal) so the wording stays identical across
// admin/api/public surfaces.
const ErrMsgNotConfigured = "Cloudflare R2 tidak dikonfigurasi."

// ErrMsgUploadFailed is the canonical user-facing message when an upload to
// the R2 backend fails. Handlers must use this constant so the wording stays
// identical across admin/api/public surfaces.
const ErrMsgUploadFailed = "Gagal mengupload file ke Cloudflare R2"

// ErrCodeNotConfigured is the machine-readable code paired with
// ErrMsgNotConfigured. Clients must branch on this code (never on the message
// text) so a wording change can never break their logic.
const ErrCodeNotConfigured = "R2_NOT_CONFIGURED"

// ErrCodeUploadFailed is the machine-readable code paired with
// ErrMsgUploadFailed.
const ErrCodeUploadFailed = "UPLOAD_FAILED"

// ErrMsgSignURLFailed is the canonical user-facing message when generating a
// signed download URL from an ENABLED backend fails. Handlers must use this
// constant so the wording stays identical across admin/api/public surfaces.
const ErrMsgSignURLFailed = "Gagal menghasilkan URL unduhan."

// ErrCodeSignURLFailed is the machine-readable code paired with
// ErrMsgSignURLFailed.
const ErrCodeSignURLFailed = "SIGNED_URL_FAILED"

// SignedURLOptions customizes the response a presigned GET returns. Zero
// values leave the object's stored metadata untouched.
type SignedURLOptions struct {
	// ContentDisposition overrides the Content-Disposition response header
	// (e.g. `attachment; filename="ujian.pdf"` to force a download).
	ContentDisposition string
	// ContentType overrides the Content-Type response header.
	ContentType string
}

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
	// SignedURLWithOptions is SignedURL with response-header overrides baked
	// into the signature (S3/R2 reject response overrides added after signing).
	SignedURLWithOptions(ctx context.Context, key string, ttl time.Duration, opts SignedURLOptions) (string, error)
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
	S3       *s3.Client
	Bucket   string
	uploader *manager.Uploader
	enabled  bool
}

// newUploader builds the multipart uploader used for large objects. The
// manager falls back to a single PutObject for small files, so APK/PDF uploads
// keep their previous behaviour while installers are split into retryable
// parts instead of one fragile ~100 MB PUT.
func newUploader(client *s3.Client) *manager.Uploader {
	return manager.NewUploader(client, func(u *manager.Uploader) {
		u.PartSize = r2PartSize
		u.Concurrency = r2UploadConcurrency
		u.LeavePartsOnError = false
		// R2 rejects the SDK's default CRC32 streaming trailers on multipart
		// UploadPart requests (500 InternalError). Disable automatic checksums
		// unless a caller explicitly asks for one.
		u.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
	})
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
		// Same R2 compatibility fix for single-part PutObject requests.
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
	})

	log.Println("r2: connected — PDF will be served via Cloudflare R2")
	return &S3Client{
		S3:       client,
		Bucket:   bucket,
		uploader: newUploader(client),
		enabled:  true,
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

// UploadWithContentType streams reader into R2 with the given key and content
// type. Large readers are uploaded in parallel multipart chunks, so a ~100 MB
// installer is not buffered fully in memory nor sent as one fragile PUT.
func (c *S3Client) UploadWithContentType(ctx context.Context, key string, reader io.Reader, contentType string) error {
	if c == nil || c.S3 == nil {
		return fmt.Errorf("r2 upload: client not configured")
	}
	uploader := c.uploader
	if uploader == nil {
		uploader = newUploader(c.S3)
	}

	_, err := uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.Bucket),
		Key:         aws.String(key),
		Body:        reader,
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("r2 upload: %w", err)
	}
	log.Printf("r2: uploaded %q (%s)", key, contentType)
	return nil
}

// UploadBytes uploads raw bytes to R2 with the given key.
func (c *S3Client) UploadBytes(ctx context.Context, key string, data []byte) error {
	return c.UploadWithContentType(ctx, key, bytes.NewReader(data), "application/pdf")
}

// SignedURL generates a temporary download URL valid for the given duration.
func (c *S3Client) SignedURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	return c.SignedURLWithOptions(ctx, key, ttl, SignedURLOptions{})
}

// SignedURLWithOptions generates a temporary GET URL with response-header
// overrides. The overrides MUST be part of the presign input: S3 SigV4 (and
// R2) signs the canonical query string, so appending response-content-* after
// signing produces a rejected URL.
func (c *S3Client) SignedURLWithOptions(ctx context.Context, key string, ttl time.Duration, opts SignedURLOptions) (string, error) {
	presign := s3.NewPresignClient(c.S3)
	input := &s3.GetObjectInput{
		Bucket: aws.String(c.Bucket),
		Key:    aws.String(key),
	}
	if opts.ContentDisposition != "" {
		input.ResponseContentDisposition = aws.String(opts.ContentDisposition)
	}
	if opts.ContentType != "" {
		input.ResponseContentType = aws.String(opts.ContentType)
	}

	req, err := presign.PresignGetObject(ctx, input, func(po *s3.PresignOptions) {
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
