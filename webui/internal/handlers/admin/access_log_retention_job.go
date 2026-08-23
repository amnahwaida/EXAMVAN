package admin

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/models"
)

// Defaults for the access-log retention job, used when the corresponding
// saas_settings keys are absent (or before the settings table is seeded).
// SuperAdmin can tune them live via saas_settings — no restart needed, because
// the loop re-reads the interval every cycle and each purge pass re-reads the
// retention window.
const (
	defaultAccessLogRetentionDays            = 90
	defaultAccessLogRetentionIntervalMinutes = 60
	minAccessLogRetentionIntervalMinutes     = 5
)

// StartAccessLogRetentionJob runs a background loop that periodically deletes
// student_access_logs rows older than the configured window
// (models.PurgeOldStudentAccessLogs). This is the Lapis-1 counterpart of the
// heartbeat pipeline: heartbeats flow Redis → heartbeat-flusher → this table
// at ~1 row/device/minute during an exam, so without a scheduled sweep the
// table (and the disk partition under it — thin-client deployments ship a
// 13 GB root filesystem) grows unbounded through exam season.
//
// Retention window : saas_settings key access_log_retention_days (default 90).
//
//	Setting it to 0 DISABLES purging entirely ("0" must never
//	mean "delete everything").
//
// Loop interval    : saas_settings key access_log_retention_interval_minutes
//
//	(default 60, clamped to a 5-minute floor so a
//	misconfigured value cannot hammer the DB with DELETEs).
//
// Runs one pass immediately, then waits the configured interval before each
// next pass. The loop stops when ctx is canceled (during graceful shutdown).
// Safe to call with a nil pool (no-op).
func StartAccessLogRetentionJob(ctx context.Context, pool *pgxpool.Pool) {
	if pool == nil {
		return
	}

	go func() {
		runAccessLogRetentionPass(ctx, pool)
		for {
			interval := time.Duration(accessLogRetentionIntervalMinutes(ctx, pool)) * time.Minute
			timer := time.NewTimer(interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				log.Println("access-log-retention job: stopped")
				return
			case <-timer.C:
				runAccessLogRetentionPass(ctx, pool)
			}
		}
	}()
	log.Printf("access-log-retention job: started (interval from saas_settings, default %d min)",
		defaultAccessLogRetentionIntervalMinutes)
}

// accessLogRetentionIntervalMinutes reads the configured sweep interval from
// saas_settings, clamped to a sane minimum so a misconfigured value cannot
// make the loop spin DELETEs against the DB.
func accessLogRetentionIntervalMinutes(ctx context.Context, pool *pgxpool.Pool) int {
	m := models.GetSaasSettingInt(ctx, pool, models.SettingAccessLogRetentionIntervalMinutes,
		defaultAccessLogRetentionIntervalMinutes)
	if m < minAccessLogRetentionIntervalMinutes {
		return minAccessLogRetentionIntervalMinutes
	}
	return m
}

// runAccessLogRetentionPass executes one purge and logs what it removed.
// Failures are logged and skipped — the next tick retries. A retention window
// of <= 0 turns the pass into an explicit no-op (retention disabled), never a
// full-table delete.
func runAccessLogRetentionPass(ctx context.Context, pool *pgxpool.Pool) {
	if pool == nil {
		return
	}
	days := models.GetSaasSettingInt(ctx, pool, models.SettingAccessLogRetentionDays,
		defaultAccessLogRetentionDays)
	if days <= 0 {
		return // retention disabled via setting
	}

	purged, err := models.PurgeOldStudentAccessLogs(ctx, pool, days)
	if err != nil {
		log.Printf("access-log-retention job: purge rows older than %d days: %v", days, err)
		return
	}
	if purged > 0 {
		log.Printf("access-log-retention job: purged %d access-log rows older than %d days", purged, days)
	}
}
