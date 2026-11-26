package r2

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type fakeS3 struct {
	mu        sync.Mutex
	parts     map[int][]byte
	singlePut int
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	q := r.URL.Query()

	switch {
	case r.Method == http.MethodPost && q.Has("uploads"):
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?><InitiateMultipartUploadResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Bucket>bucket</Bucket><Key>key</Key><UploadId>upload-1</UploadId></InitiateMultipartUploadResult>`)
	case r.Method == http.MethodPut && q.Get("uploadId") != "":
		n, _ := strconv.Atoi(q.Get("partNumber"))
		f.mu.Lock()
		f.parts[n] = body
		f.mu.Unlock()
		w.Header().Set("ETag", fmt.Sprintf(`"part-%d"`, n))
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodPost && q.Get("uploadId") != "":
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?><CompleteMultipartUploadResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Bucket>bucket</Bucket><Key>key</Key><ETag>"final"</ETag></CompleteMultipartUploadResult>`)
	case r.Method == http.MethodPut:
		f.mu.Lock()
		f.singlePut++
		f.mu.Unlock()
		w.Header().Set("ETag", `"single"`)
		w.WriteHeader(http.StatusOK)
	default:
		http.Error(w, "unexpected request", http.StatusNotImplemented)
	}
}

func newTestS3Client(t *testing.T, endpoint string) *S3Client {
	t.Helper()
	cfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion("auto"),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
	)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	})
	return &S3Client{S3: client, Bucket: "bucket", uploader: newUploader(client), enabled: true}
}

func TestNewUploaderR2Compatibility(t *testing.T) {
	client := newTestS3Client(t, "http://127.0.0.1")
	uploader := newUploader(client.S3)
	if uploader.RequestChecksumCalculation != aws.RequestChecksumCalculationWhenRequired {
		t.Fatalf("uploader checksum calculation = %q, want when_required", uploader.RequestChecksumCalculation)
	}
	if uploader.Concurrency > 3 {
		t.Fatalf("uploader concurrency = %d, want <= 3 for R2", uploader.Concurrency)
	}
}

func TestUploadWithContentTypeUsesMultipartForLargeReader(t *testing.T) {
	fake := &fakeS3{parts: map[int][]byte{}}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	client := newTestS3Client(t, srv.URL)

	data := bytes.Repeat([]byte("examvan-installer-"), (17*1024*1024)/16+1)[:17*1024*1024+123]
	if err := client.UploadWithContentType(context.Background(), "apps/windows/1.0.0/setup.exe", bytes.NewReader(data), "application/x-msdownload"); err != nil {
		t.Fatalf("UploadWithContentType: %v", err)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.singlePut != 0 {
		t.Fatalf("large upload used %d single PUT(s), want multipart", fake.singlePut)
	}
	if len(fake.parts) != 2 {
		t.Fatalf("uploaded %d parts, want 2", len(fake.parts))
	}
	nums := make([]int, 0, len(fake.parts))
	for n := range fake.parts {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	var got []byte
	for _, n := range nums {
		got = append(got, fake.parts[n]...)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("multipart payload mismatch: got %d bytes, want %d", len(got), len(data))
	}
}

func TestUploadWithContentTypeKeepsSinglePutForSmallReader(t *testing.T) {
	fake := &fakeS3{parts: map[int][]byte{}}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	client := newTestS3Client(t, srv.URL)

	if err := client.UploadWithContentType(context.Background(), "apps/android/1.0.0/app.apk", bytes.NewReader([]byte("small-apk")), "application/vnd.android.package-archive"); err != nil {
		t.Fatalf("UploadWithContentType: %v", err)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.singlePut != 1 {
		t.Fatalf("single PUT count = %d, want 1", fake.singlePut)
	}
	if len(fake.parts) != 0 {
		t.Fatalf("small upload used %d multipart parts, want 0", len(fake.parts))
	}
}
