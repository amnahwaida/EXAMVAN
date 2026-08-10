package admin

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/models"
)

// Defaults for the approval-cleanup job, used when the corresponding
// saas_settings keys are absent (or before the settings table is seeded).
// SuperAdmin can tune them live from the SaaS settings panel — no restart
// needed, because the loop re-reads the interval every cycle and the purge
// pass re-reads the two staleness windows on every run.
const (
	defaultApprovalCleanupIntervalMinutes = 15
	defaultApprovalCleanupEndedGraceHours = 1
	defaultApprovalCleanupInactiveTTLHours = 24
	minApprovalCleanupIntervalMinutes     = 1
)

// StartApprovalCleanupJob runs a background loop that periodically purges
// stale exam_approvals rows (see models.PurgeStaleExamApprovals): pending
// rows that can never be decided (their exam ended) and pending/approved rows
// on exams that have been inactive for a long time. This bounds the queue a
// spam flood could otherwise grow, and frees per-exam approved-device cap
// slots held by devices that were approved but never submitted once the exam
// is over — so a reused exam starts with a clean slate.
//
// Runs one pass immediately, then waits the configured interval
// (approval_cleanup_interval_minutes, default 15) before each next pass. The
// loop stops when ctx is canceled (during graceful shutdown). Safe to call
// with a nil pool (no-op).
func StartApprovalCleanupJob(ctx context.Context, pool *pgxpool.Pool) {
	if pool == nil {
		return
	}

	go func() {
		runApprovalCleanupPass(ctx, pool)
		for {
			interval := time.Duration(approvalCleanupIntervalMinutes(ctx, pool)) * time.Minute
			timer := time.NewTimer(interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				log.Println("approval-cleanup job: stopped")
				return
			case <-timer.C:
				runApprovalCleanupPass(ctx, pool)
			}
		}
	}()
	log.Printf("approval-cleanup job: started (interval from saas_settings, default %d min)",
		defaultApprovalCleanupIntervalMinutes)
}

// approvalCleanupIntervalMinutes reads the configured purge interval from
// saas_settings, clamped to a sane minimum so a misconfigured value cannot
// make the loop spin on the DB.
func approvalCleanupIntervalMinutes(ctx context.Context, pool *pgxpool.Pool) int {
	m := models.GetSaasSettingInt(ctx, pool, models.SettingApprovalCleanupIntervalMinutes,
		defaultApprovalCleanupIntervalMinutes)
	if m < minApprovalCleanupIntervalMinutes {
		return minApprovalCleanupIntervalMinutes
	}
	return m
}

// runApprovalCleanupPass executes one purge and logs what it removed.
// Failures are logged and skipped — the next tick retries. Idempotent by
// construction: rows that no longer match a rule are simply not selected.
// The staleness windows come from saas_settings (grace after an exam ends,
// TTL for rows on inactive exams) so a SuperAdmin can tighten the purge
// without a redeploy.
func runApprovalCleanupPass(ctx context.Context, pool *pgxpool.Pool) {
	if pool == nil {
		return
	}
	endedGrace := models.GetSaasSettingInt(ctx, pool, models.SettingApprovalCleanupEndedGraceHours,
		defaultApprovalCleanupEndedGraceHours)
	inactiveTTL := models.GetSaasSettingInt(ctx, pool, models.SettingApprovalCleanupInactiveTTLHours,
		defaultApprovalCleanupInactiveTTLHours)
	if endedGrace < 0 {
		endedGrace = 0
	}
	if inactiveTTL < 0 {
		inactiveTTL = 0
	}

	stats, err := models.PurgeStaleExamApprovals(ctx, pool, endedGrace, inactiveTTL)
	if err != nil {
		log.Printf("approval-cleanup job: purge stale approvals: %v", err)
		return
	}
	if total := stats.Total(); total > 0 {
		log.Printf("approval-cleanup job: purged %d stale approval rows "+
			"(pending ended=%d, approved ended=%d, pending inactive=%d, approved inactive=%d)",
			total, stats.PendingEnded, stats.ApprovedEnded, stats.PendingInactive, stats.ApprovedInactive)
	}
}
