package queue

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
)

// ---------------------------------------------------------------------------
// M5 — retryOrFail spool
//
// retryOrFail swallowed the error of the re-enqueue (_ =
// EnqueueSubmissionWithJob): when Redis failed exactly at the moment a
// failed job had to be pushed back, the job — the student's answers — was
// gone FOREVER: the result key "failed" only lives 5 minutes (resultTTL),
// so nothing remained. The intended contract:
//
//   - when the re-enqueue of a job still inside its retry budget fails,
//     the job is durably spooled on disk as JSON (one file per job,
//     named <jobID>.json inside the configured spool dir);
//   - DrainSubmissionSpool re-enqueues every spooled job and removes the
//     file on success (answers are never lost);
//   - when Redis is unavailable, DrainSubmissionSpool is a no-op
//     (nothing is lost, nothing duplicated).
// ---------------------------------------------------------------------------

func writeSpoolFile(t *testing.T, dir string, job *SubmissionJob) {
	t.Helper()
	payload, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("marshal spool job: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, job.JobID+".json"), payload, 0o600); err != nil {
		t.Fatalf("write spool file: %v", err)
	}
}

func readSpoolDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read spool dir: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	return names
}

// TestRetryOrFailSpoolsJobWhenReenqueueFails pins the durable fallback: a
// retry-budgeted job whose re-enqueue fails lands on disk instead of
// disappearing with only the 5-minute result key as a trace.
func TestRetryOrFailSpoolsJobWhenReenqueueFails(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	dir := t.TempDir()
	w := &Worker{rdb: rdb, pool: nil, submissionSpoolDir: dir}

	// Make every Redis command fail, as during a Redis incident.
	mr.SetError("LOADING Redis is loading the dataset in memory")

	job := &SubmissionJob{
		JobID: "m5spool1", ExamID: 1, StudentName: "Siswa Spool",
		ExamNumber: "07", MACAddress: "AA:BB:CC:DD:EE:07",
		Retries: 1, EnqueuedAt: time.Now().UTC().Format(time.RFC3339),
	}
	w.retryOrFail(&SubmissionResult{Job: *job}, "insert failed (simulated)")

	names := readSpoolDir(t, dir)
	if len(names) != 1 {
		t.Fatalf("spool files = %v, want exactly 1 (the job must survive the failed re-enqueue)", names)
	}
	if names[0] != "m5spool1.json" {
		t.Fatalf("spool file = %s, want m5spool1.json", names[0])
	}
}

// TestRetryOrFailSuccessfulReenqueueDoesNotSpool pins that the happy path
// does not touch the spool: only a FAILED re-enqueue may write a file.
func TestRetryOrFailSuccessfulReenqueueDoesNotSpool(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	dir := t.TempDir()
	w := &Worker{rdb: rdb, pool: nil, submissionSpoolDir: dir}

	job := &SubmissionJob{
		JobID: "m5spool-ok", ExamID: 1, StudentName: "Siswa Ok",
		ExamNumber: "10", MACAddress: "AA:BB:CC:DD:EE:10",
		Retries: 1, EnqueuedAt: time.Now().UTC().Format(time.RFC3339),
	}
	w.retryOrFail(&SubmissionResult{Job: *job}, "insert failed (simulated)")

	if names := readSpoolDir(t, dir); len(names) != 0 {
		t.Fatalf("spool files after successful re-enqueue = %v, want none", names)
	}
	if n, _ := rdb.LLen(context.Background(), QueueKey).Result(); n != 1 {
		t.Fatalf("queue len = %d, want 1 (normal retry path untouched)", n)
	}
}

// TestDrainSubmissionSpoolReenqueuesAndClears pins the recovery side: spooled
// jobs are re-enqueued and their files removed once Redis accepts them again
// — the student's answers reach the scoring pipeline eventually.
func TestDrainSubmissionSpoolReenqueuesAndClears(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	dir := t.TempDir()
	job := &SubmissionJob{
		JobID: "m5spool2", ExamID: 2, StudentName: "Siswa Spool 2",
		ExamNumber: "08", MACAddress: "AA:BB:CC:DD:EE:08",
		Retries: 1, EnqueuedAt: time.Now().UTC().Format(time.RFC3339),
	}
	writeSpoolFile(t, dir, job)

	drained, err := DrainSubmissionSpool(rdb, dir)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if drained != 1 {
		t.Fatalf("drained = %d, want 1", drained)
	}

	// The job must be back on the queue.
	n, err := rdb.LLen(context.Background(), QueueKey).Result()
	if err != nil || n != 1 {
		t.Fatalf("queue len = %d err=%v, want 1", n, err)
	}

	// And the spool file must be gone.
	if names := readSpoolDir(t, dir); len(names) != 0 {
		t.Fatalf("spool files after drain = %v, want none", names)
	}
}

// TestDrainSubmissionSpoolRedisUnavailableIsNoop: draining into a dead Redis
// must not delete the spool files (nothing lost, nothing duplicated).
func TestDrainSubmissionSpoolRedisUnavailableIsNoop(t *testing.T) {
	dir := t.TempDir()
	job := &SubmissionJob{
		JobID: "m5spool3", ExamID: 3, StudentName: "Siswa Spool 3",
		ExamNumber: "09", MACAddress: "AA:BB:CC:DD:EE:09",
		Retries: 0, EnqueuedAt: time.Now().UTC().Format(time.RFC3339),
	}
	writeSpoolFile(t, dir, job)

	// A nil client models "Redis unavailable" exactly like
	// isRedisUnavailable treats it in production code.
	drained, err := DrainSubmissionSpool(nil, dir)
	if err != nil {
		t.Fatalf("drain with nil redis: %v", err)
	}
	if drained != 0 {
		t.Fatalf("drained = %d, want 0", drained)
	}
	if names := readSpoolDir(t, dir); len(names) != 1 {
		t.Fatalf("spool files must survive a failed drain, got %v", names)
	}
}

// TestDrainSubmissionSpoolMissingDirIsNoop: no spool dir (fresh install /
// never spooled) must not error.
func TestDrainSubmissionSpoolMissingDirIsNoop(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	drained, err := DrainSubmissionSpool(rdb, filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("drain on missing dir: %v", err)
	}
	if drained != 0 {
		t.Fatalf("drained = %d, want 0", drained)
	}
}
