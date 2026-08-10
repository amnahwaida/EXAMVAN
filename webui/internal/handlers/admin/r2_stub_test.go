package admin

import (
	"context"
	"io"
	"time"

	r2client "github.com/examvan/webui/internal/handlers/r2"
)

// stubR2 is an in-memory r2.Client for handler tests: it records the keys
// passed to Upload/Delete (so tests can assert the right R2 keys are
// touched) and can be switched to a failing backend. No network involved —
// this is exactly why r2.Client is an interface.
type stubR2 struct {
	enabled        bool
	uploads        []string
	deletes        []string
	failWith       error
	deleteFailWith error // fails Delete only, leaving Upload working (orphan-cleanup path)
}

var _ r2client.Client = (*stubR2)(nil)

func newStubR2() *stubR2 {
	return &stubR2{enabled: true}
}

func (s *stubR2) Enabled() bool { return s.enabled }

func (s *stubR2) Upload(ctx context.Context, key string, reader io.Reader) error {
	return s.UploadWithContentType(ctx, key, reader, "application/pdf")
}

func (s *stubR2) UploadWithContentType(ctx context.Context, key string, reader io.Reader, contentType string) error {
	s.uploads = append(s.uploads, key)
	return s.failWith
}

func (s *stubR2) UploadBytes(ctx context.Context, key string, data []byte) error {
	s.uploads = append(s.uploads, key)
	return s.failWith
}

func (s *stubR2) SignedURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if s.failWith != nil {
		return "", s.failWith
	}
	return "https://storage.example.test/" + key, nil
}

func (s *stubR2) Delete(ctx context.Context, key string) error {
	s.deletes = append(s.deletes, key)
	if s.deleteFailWith != nil {
		return s.deleteFailWith
	}
	return s.failWith
}

// containsString reports whether the slice holds s.
func containsString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
