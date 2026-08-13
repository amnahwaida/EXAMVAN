package admin

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/models"
)

// packageExpiryJobInterval is how often the background reconciliation pass
// runs. Mirrors the heartbeat flusher cadence.
const packageExpiryJobInterval = 30 * time.Second

// expiryJobAdvisoryLockKey is the PostgreSQL advisory lock that serializes
// the expiry reconciliation pass across the whole cluster: only ONE
// instance/replica runs a pass at a time. Two concurrent passes would each
// select the same exhausted packages and fight over the redemption rows
// (pause vs. recheck), so a second pass must SKIP instead of racing.
const expiryJobAdvisoryLockKey = 0x45585031 // "EXP1" as an int64 key

// expiredRunningExamGrace is how long a running exam of an expired account is
// allowed to continue before tombstoneExpiredUsersExamsPass cuts it off.
// Unstarted active exams are tombstoned immediately on expiry; running exams
// get this window so students mid-exam are not disconnected at the exact
// expiry instant, giving the operator time to renew or the session time to
// wrap up.
const expiredRunningExamGrace = 24 * time.Hour

// StartPackageExpiryJob runs a background loop that reconciles claimed voucher
// packages whose lifetime has run out. A user can hold several claimed
// vouchers, but only one ACTIVE package consumes lifetime; when that active
// package expires:
//
//  1. it is paused (remaining_seconds = 0, is_active = false);
//  2. if the user still holds ANOTHER claimed voucher with remaining lifetime,
//     that package is activated automatically (auto-fallback) and its
//     entitlement snapshot is applied, extending the account expiry — the user
//     is never locked out while a usable package is on hand. An account whose
//     expires_at is NULL (unlimited — e.g. an admin cleared it in the edit
//     form) is the one exception: its exhausted package is paused WITHOUT a
//     fallback, so the admin's "unlimited" intent is never silently undone
//     by a package clock;
//  3. if no usable fallback exists, the account keeps its past expires_at and
//     login stays blocked;
//  4. accounts whose expiry still reads in the past after the reconciliation
//     (trial/personal and any account without a usable fallback) have their
//     active-but-unstarted exams tombstoned (tombstoneExpiredUsersExamsPass),
//     mirroring policy B for schools.
//
// Each tick runs the package pass first, then the tombstone pass, so a user
// whose fallback was auto-activated (expiry re-opened) is spared by the
// tombstone. Runs one pass immediately, then every packageExpiryJobInterval.
// The loop stops when ctx is canceled (during graceful shutdown). Safe to
// call with a nil pool (no-op).
func StartPackageExpiryJob(ctx context.Context, pool *pgxpool.Pool) {
	if pool == nil {
		return
	}

	go func() {
		runPackageExpiryPass(ctx, pool)
		tombstoneExpiredUsersExamsPass(ctx, pool)
		ticker := time.NewTicker(packageExpiryJobInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				log.Println("package-expiry job: stopped")
				return
			case <-ticker.C:
				runPackageExpiryPass(ctx, pool)
				tombstoneExpiredUsersExamsPass(ctx, pool)
			}
		}
	}()
	log.Printf("package-expiry job: started (every %s)", packageExpiryJobInterval)
}

// tombstoneExpiredUsersExamsPass (policy B for expired accounts) finds every
// non-suspended, non-superadmin user whose account expiry has passed and sets
// their active exams inactive with the tombstone marker — the same "unpublished
// exams go dormant" semantics as the school tombstone
// (tombstoneUnstartedInstansiExams), now applied to trial/personal and any
// other account that ran out of active time. The tombstone is never
// auto-reversed: the owner (after a renewal) or a superadmin re-activates
// manually, so an admin's explicit inactivation is never clobbered.
//
// A running exam (exam_started_at set) is NOT cut off the moment the account
// expires: it gets expiredRunningExamGrace (24h) to finish, so students
// mid-exam are not disconnected at the exact expiry instant. Only after the
// grace elapses is the running exam tombstoned (exam_started_at cleared so the
// Android app can no longer continue it). Unstarted active exams are
// tombstoned immediately.
//
// The pass must run AFTER runPackageExpiryPass: a user whose active voucher
// package expired but who still holds a usable claimed voucher is re-opened
// by the auto-fallback (expires_at extended into the future) in that pass, so
// their exams are spared here — only accounts that stay expired get
// tombstoned. Suspended accounts are skipped because their clock is frozen
// (their expiry is extended back on reactivation).
//
// The rule is continuous, not event-driven: while an account stays expired,
// re-activating one of its tombstoned exams only lasts until the next pass
// re-tombstones it — a re-activation sticks only after the account is renewed
// (expires_at in the future). Expired users who still hold a claimed-but-
// UNACTIVATED voucher are also covered: their account expiry has passed, so
// their exams go dormant too, consistent with policy B where a package end is
// never auto-reversed.
func tombstoneExpiredUsersExamsPass(ctx context.Context, pool *pgxpool.Pool) {
	if pool == nil {
		return
	}
	// tombstoned_at marks the exam as auto-inactivated (policy B) so the admin
	// UI can tell it apart from a manual inactivation; the marker is cleared
	// whenever the exam is (re)activated. exam_started_at is cleared so a
	// cut-off running exam does not keep its "running" state (and its token is
	// no longer usable for a fresh start). The status='active' filter makes the
	// pass idempotent — a tombstoned exam is inactive and is never re-selected.
	//
	// Running exams get a grace window (exam_started_at IS NOT NULL but the
	// account expired within the grace) so students already taking the exam are
	// not disconnected mid-test; only unstarted exams — or running exams past
	// the grace — are tombstoned.
	_, err := pool.Exec(ctx, `
		UPDATE exams e
		SET status = 'inactive', tombstoned_at = now(), exam_started_at = NULL
		FROM admin_users u
		WHERE e.created_by = u.id
		  AND e.status = 'active'
		  AND e.tombstoned_at IS NULL
		  AND u.status = 'active' -- suspended accounts: clock is frozen
		  AND u.expires_at IS NOT NULL
		  AND u.expires_at < now()
		  AND (
		      e.exam_started_at IS NULL
		      OR u.expires_at < now() - ($1 * interval '1 hour')
		  )
		  AND NOT (u.role = 'superadmin' OR u.role ILIKE '%"superadmin"%')`,
		int(expiredRunningExamGrace.Hours()))
	if err != nil {
		log.Printf("package-expiry job: tombstone expired users' exams: %v", err)
	}
}

// runPackageExpiryPass finds every user with an exhausted active package and
// reconciles it (see StartPackageExpiryJob). Each user is handled in its own
// transaction. The pass runs under a cluster-wide advisory lock
// (expiryJobAdvisoryLockKey): a second replica/loop instance calling the pass
// while the lock is held SKIPS immediately (returns false) instead of racing
// over the same redemption rows. Returns true when this call actually ran the
// reconciliation.
func runPackageExpiryPass(ctx context.Context, pool *pgxpool.Pool) bool {
	// Take the advisory lock on a DEDICATED pool connection: advisory locks
	// are per-backend-session, and a pooled QueryRow may land on ANY backend —
	// acquiring on one pooled connection and releasing on another would leak
	// the lock (the holder backend would keep it until it closes, silently
	// skipping every later pass). The dedicated connection pins lock-take,
	// lock-hold, and lock-release to the SAME backend, and it is returned to
	// the pool on exit (the connection release also drops the lock).
	lockConn, err := pool.Acquire(ctx)
	if err != nil {
		log.Printf("package-expiry job: acquire lock connection failed: %v", err)
		return false
	}
	defer lockConn.Release()

	var acquired bool
	if err := lockConn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, expiryJobAdvisoryLockKey).Scan(&acquired); err != nil {
		log.Printf("package-expiry job: advisory lock check failed: %v", err)
		return false
	}
	if !acquired {
		log.Printf("package-expiry job: another pass holds the advisory lock — skipping this tick")
		return false
	}
	defer func() {
		// Best-effort release on the SAME backend that holds the lock.
		var released bool
		if err := lockConn.QueryRow(ctx, `SELECT pg_advisory_unlock($1)`, expiryJobAdvisoryLockKey).Scan(&released); err != nil {
			log.Printf("package-expiry job: advisory lock release failed: %v", err)
		}
	}()

	// Only the active package consumes lifetime, so an active redemption whose
	// remaining_seconds has been fully elapsed by activated_at is exhausted.
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT r.user_id
		FROM voucher_redemptions r
		JOIN admin_users u ON u.id = r.user_id
		WHERE r.is_active
		  AND u.status <> 'suspended' -- suspended accounts: clock is frozen
		  AND r.activated_at IS NOT NULL
		  AND r.remaining_seconds - COALESCE(EXTRACT(EPOCH FROM (now() - r.activated_at))::bigint, 0) <= 0`)
	if err != nil {
		log.Printf("package-expiry job: query expired packages: %v", err)
		return true
	}
	defer rows.Close()

	var userIDs []int
	for rows.Next() {
		var uid int
		if err := rows.Scan(&uid); err != nil {
			log.Printf("package-expiry job: scan user: %v", err)
			return true
		}
		userIDs = append(userIDs, uid)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Printf("package-expiry job: iterate users: %v", err)
		return true
	}

	for _, uid := range userIDs {
		handleExpiredPackage(ctx, pool, uid)
	}
	return true
}

// handleExpiredPackage pauses the user's exhausted active package and, when a
// usable alternative exists, activates it (auto-fallback) and applies its
// entitlement snapshot to the account. Runs inside a transaction that locks
// the user row so concurrent redeem/activate requests serialize cleanly.
//
// An account whose expires_at is NULL (unlimited) is spared the fallback: the
// exhausted package is paused and that is all — activating a fallback would
// rewrite expires_at to a concrete value (applyRedemptionEntitlement sets it
// unconditionally), silently re-limiting an account an admin made unlimited.
func handleExpiredPackage(ctx context.Context, pool *pgxpool.Pool, userID int) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		log.Printf("package-expiry job: begin tx (user %d): %v", userID, err)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Lock the user row so a concurrent redeem/activate cannot interleave.
	// Skip suspended accounts: their package clock is frozen, so their
	// packages must not be expired/auto-fallback-processed mid-suspension.
	// expires_at is read under the same lock so the unlimited-account guard
	// below sees a value no concurrent writer can change.
	var locked int
	var expiresAt *time.Time
	if err := tx.QueryRow(ctx,
		`SELECT id, expires_at FROM admin_users WHERE id = $1 AND status <> 'suspended' FOR UPDATE`,
		userID).Scan(&locked, &expiresAt); err != nil {
		if err != pgx.ErrNoRows {
			log.Printf("package-expiry job: lock user %d: %v", userID, err)
		}
		return
	}

	// Re-check under the lock: the active redemption must still be expired
	// (another pass or an in-flight activate may have handled it already).
	var activeID int
	err = tx.QueryRow(ctx, `
		SELECT r.id
		FROM voucher_redemptions r
		WHERE r.user_id = $1 AND r.is_active AND r.activated_at IS NOT NULL
		  AND r.remaining_seconds - COALESCE(EXTRACT(EPOCH FROM (now() - r.activated_at))::bigint, 0) <= 0
		FOR UPDATE`, userID).Scan(&activeID)
	if err == pgx.ErrNoRows {
		return // nothing to do
	}
	if err != nil {
		log.Printf("package-expiry job: recheck active (user %d): %v", userID, err)
		return
	}

	// 1. Pause the exhausted package (lifetime fully consumed).
	if _, err := tx.Exec(ctx, `
		UPDATE voucher_redemptions
		SET remaining_seconds = 0, activated_at = NULL, is_active = false
		WHERE id = $1`, activeID); err != nil {
		log.Printf("package-expiry job: pause expired (user %d): %v", userID, err)
		return
	}

	// An account without an expiry (NULL = unlimited, e.g. an admin cleared it
	// in the edit form) must not be re-limited by an auto-fallback: with no
	// expiry there is nothing for the package clock to align to, so the
	// exhausted package is paused and that is all — the account stays
	// unlimited and any claimed-but-paused fallback stays paused. Committing
	// here persists the pause (a bare return would roll it back and re-select
	// this user every pass).
	if expiresAt == nil {
		if cerr := tx.Commit(ctx); cerr != nil {
			log.Printf("package-expiry job: commit pause (unlimited user %d): %v", userID, cerr)
		}
		return
	}

	// 2. Pick the best usable fallback: a paused claimed voucher that still has
	// remaining lifetime, preferring the one with the most time left.
	var r models.VoucherRedemption
	err = tx.QueryRow(ctx, `
		SELECT id, voucher_id, user_id, redeemed_at, remaining_seconds, activated_at, is_active,
		       COALESCE(package, ''), COALESCE(max_exams, 0),
		       COALESCE(max_pdf_size, 0), COALESCE(max_concurrent_exams, 0),
		       COALESCE(max_storage_size, 0), COALESCE(role, '')
		FROM voucher_redemptions
		WHERE user_id = $1 AND NOT is_active AND remaining_seconds > 0
		ORDER BY remaining_seconds DESC, redeemed_at ASC, id ASC
		LIMIT 1
		FOR UPDATE`, userID).Scan(
		&r.ID, &r.VoucherID, &r.UserID, &r.RedeemedAt, &r.RemainingSeconds, &r.ActivatedAt, &r.IsActive,
		&r.Package, &r.MaxExams, &r.MaxPDFSize, &r.MaxConcurrentExams,
		&r.MaxStorageSize, &r.Role,
	)
	if err == pgx.ErrNoRows {
		// No usable fallback — persist the pause of the exhausted package (the
		// account keeps its past expires_at and login stays blocked). Committing
		// here matters: a bare return would roll the pause back, leaving the
		// redemption active-with-zero-lifetime and re-selecting this user on
		// every pass.
		if cerr := tx.Commit(ctx); cerr != nil {
			log.Printf("package-expiry job: commit pause (user %d): %v", userID, cerr)
		}
		return
	}
	if err != nil {
		log.Printf("package-expiry job: find fallback (user %d): %v", userID, err)
		return
	}

	// 3. Activate the fallback and apply its snapshot (quota, merged role, and
	// expires_at = now + remaining_seconds, which re-opens login).
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, `
		UPDATE voucher_redemptions SET is_active = true, activated_at = $2
		WHERE id = $1`, r.ID, now); err != nil {
		log.Printf("package-expiry job: activate fallback (user %d): %v", userID, err)
		return
	}
	r.ActivatedAt = &now
	if err := applyRedemptionEntitlement(ctx, tx, userID, &r); err != nil {
		log.Printf("package-expiry job: apply entitlement (user %d): %v", userID, err)
		return
	}

	if err := tx.Commit(ctx); err != nil {
		log.Printf("package-expiry job: commit (user %d): %v", userID, err)
		return
	}

	log.Printf("package-expiry job: user %d's package expired → auto-activated %q with %s remaining",
		userID, r.Package, time.Duration(r.RemainingSeconds)*time.Second)
}
