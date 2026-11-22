package admin

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/examvan/webui/internal/models"
)

// fireConcurrentUploads sends the prepared uploads at the same instant and
// returns each response status (-1 on transport error).
func fireConcurrentUploads(t *testing.T, srv *httptest.Server, clients []*http.Client, names []string, pdf []byte) []int {
	t.Helper()
	requests := make([]*http.Request, len(clients))
	for i := range clients {
		body, contentType := editExamMultipart(t, names[i], pdf, names[i]+".pdf")
		req, err := http.NewRequest(http.MethodPost, srv.URL+"/admin/api/upload", body)
		if err != nil {
			t.Fatalf("new upload request %d: %v", i, err)
		}
		req.Header.Set("Content-Type", contentType)
		requests[i] = req
	}

	start := make(chan struct{})
	results := make(chan int, len(clients))
	var wg sync.WaitGroup
	for i := range clients {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			resp, err := clients[i].Do(requests[i])
			if err != nil {
				results <- -1
				return
			}
			resp.Body.Close()
			results <- resp.StatusCode
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)

	out := make([]int, 0, len(clients))
	for status := range results {
		out = append(out, status)
	}
	return out
}

func assertOneAcceptedOneStorageRejected(t *testing.T, statuses []int) {
	t.Helper()
	ok, rejected, other := 0, 0, 0
	for _, status := range statuses {
		switch status {
		case http.StatusOK:
			ok++
		case http.StatusForbidden:
			rejected++
		default:
			other++
		}
	}
	if other > 0 {
		t.Fatalf("concurrent uploads: unexpected statuses %v", statuses)
	}
	if ok != 1 || rejected != 1 {
		t.Fatalf("concurrent uploads: want exactly 1 success + 1 storage reject, got %v", statuses)
	}
}

// TestPerAccountStorageUploadAtomicUnderConcurrency pins the per-account
// storage race: two simultaneous uploads by the same non-operator account must
// not both pass the stale storage pre-check and overshoot max_storage_size.
func TestPerAccountStorageUploadAtomicUnderConcurrency(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	userID := createSchoolQuotaUser(t, pool, "guru-storage-race", "SMK Storage Race", []string{models.RoleGuru}, 10)
	if _, err := pool.Exec(ctx,
		`UPDATE admin_users SET max_storage_size = 300, max_pdf_size = 1048576 WHERE id = $1`, userID); err != nil {
		t.Fatalf("pin storage quota: %v", err)
	}

	r, stub := newSchoolQuotaTestRouter(pool)
	stub.uploadDelay = 150 * time.Millisecond
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	client := &http.Client{Jar: jar}
	tc := &quotaTestClient{srv: srv, client: client, pool: pool}
	tc.login(t, userID)

	statuses := fireConcurrentUploads(t, srv, []*http.Client{client, client},
		[]string{"storage-race-1", "storage-race-2"}, schoolTestPDF(200))
	assertOneAcceptedOneStorageRejected(t, statuses)

	var used int64
	if err := pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(size_bytes), 0) FROM exams WHERE created_by = $1`, userID).Scan(&used); err != nil {
		t.Fatalf("sum account storage: %v", err)
	}
	if used != 200 {
		t.Fatalf("account storage after race = %d, want 200 (cap 300)", used)
	}
	if len(stub.deletes) != 1 {
		t.Fatalf("rejected upload's R2 orphan not cleaned: deletes=%v uploads=%v", stub.deletes, stub.uploads)
	}
}

// TestFamilyStorageUploadAtomicUnderConcurrency pins the pool-less operator
// path: two simultaneous uploads by the operator itself must serialize on the
// family storage budget even though its own max_exams is unlimited.
func TestFamilyStorageUploadAtomicUnderConcurrency(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	opID := createSchoolQuotaUser(t, pool, "op-storage-race", "personal", []string{models.RoleOperator}, -1)
	if _, err := pool.Exec(ctx,
		`UPDATE admin_users SET max_storage_size = 300, max_pdf_size = 1048576 WHERE id = $1`, opID); err != nil {
		t.Fatalf("pin operator storage quota: %v", err)
	}

	r, stub := newSchoolQuotaTestRouter(pool)
	stub.uploadDelay = 150 * time.Millisecond
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	client := &http.Client{Jar: jar}
	tc := &quotaTestClient{srv: srv, client: client, pool: pool}
	tc.login(t, opID)

	statuses := fireConcurrentUploads(t, srv, []*http.Client{client, client},
		[]string{"family-storage-race-1", "family-storage-race-2"}, schoolTestPDF(200))
	assertOneAcceptedOneStorageRejected(t, statuses)

	var used int64
	if err := pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(size_bytes), 0) FROM exams WHERE created_by = $1`, opID).Scan(&used); err != nil {
		t.Fatalf("sum family storage: %v", err)
	}
	if used != 200 {
		t.Fatalf("family storage after race = %d, want 200 (cap 300)", used)
	}
	if len(stub.deletes) != 1 {
		t.Fatalf("rejected upload's R2 orphan not cleaned: deletes=%v uploads=%v", stub.deletes, stub.uploads)
	}
}
