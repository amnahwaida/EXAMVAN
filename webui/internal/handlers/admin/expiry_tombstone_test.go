package admin

import (
	"context"
	"testing"
	"time"

	"github.com/examvan/webui/internal/models"
)

// ---------------------------------------------------------------------------
// Test 9: expired accounts tombstone their unstarted exams
// ---------------------------------------------------------------------------

// TestExpiredAccountTombstonesUnstartedExams locks in the policy for
// trial/personal (and any voucher-less) accounts: when an account's active
// period expires (expires_at passed while status stays 'active' — expiry is
// time-based), ALL its active exams are auto-inactivated with the tombstone
// marker — including ones already running, whose exam_started_at is cleared
// so the Android app can no longer continue them — mirroring the school
// policy B. The tombstone is never auto-reversed, and a re-activation clears
// the marker. Superadmin accounts and accounts with a future expiry are
// spared. The account itself stays able to log in (feature-locked, not
// blocked) so its owner can renew on the billing page.
func TestExpiredAccountTombstonesUnstartedExams(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	// A personal trial account (free package, no vouchers) that is already
	// expired: status stays 'active' because expiry is time-based.
	trial := createOperatorUser(t, pool, "trial1", "personal", "pass-trial1")
	if _, err := pool.Exec(ctx,
		`UPDATE admin_users SET expires_at = now() - interval '1 second' WHERE id = $1`, trial.ID); err != nil {
		t.Fatalf("expire trial account: %v", err)
	}
	// An expired account can still authenticate (login is no longer expiry-
	// gated) — it is admitted so its owner can renew on the billing page; the
	// feature lock is enforced downstream, not at login.
	if got, msg := models.AuthenticateUser(ctx, pool, "trial1", "pass-trial1"); msg != "" || got == nil {
		t.Fatalf("expired trial login must succeed (feature-locked), got msg=%q user=%+v", msg, got)
	} else if !got.IsFeatureLocked() {
		t.Fatalf("expired trial login: want IsFeatureLocked()=true, got false")
	}

	// A healthy account whose expiry is still in the future (must be spared).
	future := createOperatorUser(t, pool, "future1", "personal", "pass-future1")
	if _, err := pool.Exec(ctx,
		`UPDATE admin_users SET expires_at = now() + interval '10 days' WHERE id = $1`, future.ID); err != nil {
		t.Fatalf("extend future account: %v", err)
	}

	// A superadmin (must be spared).
	root, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "rootadmin9", Name: "Root Admin 9",
		PasswordHash: "pass-root", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}

	// Seed exams: an unstarted one by the expired trial account (must be
	// tombstoned), a running one by the same account (now also tombstoned,
	// with exam_started_at cleared so the Android app cannot continue it), and
	// unstarted ones by the future-valid account and the superadmin (both
	// must be spared).
	trialUnstarted := insertTestExam(t, pool, trial.ID, "trial-unstarted", nil)
	started := time.Now().UTC().Add(-5 * time.Minute)
	trialRunning := insertTestExam(t, pool, trial.ID, "trial-running", &started)
	futureUnstarted := insertTestExam(t, pool, future.ID, "future-unstarted", nil)
	rootUnstarted := insertTestExam(t, pool, root.ID, "root-unstarted", nil)

	// Run exactly what the background job's tick runs: the voucher package
	// reconciliation first (a no-op here — the trial account has no
	// redemptions), then the expired-account tombstone pass.
	runPackageExpiryPass(ctx, pool)
	tombstoneExpiredUsersExamsPass(ctx, pool)

	for _, e := range []struct {
		id   int
		name string
		want string
	}{{trialUnstarted, "trial-unstarted", "inactive"}, {trialRunning, "trial-running", "inactive"}, {futureUnstarted, "future-unstarted", "active"}, {rootUnstarted, "root-unstarted", "active"}} {
		if got := mustGetExam(t, pool, e.id).Status; got != e.want {
			t.Errorf("%s after pass: status=%s, want %s", e.name, got, e.want)
		}
	}
	// The tombstone marker is set on every auto-inactivated exam — and on no
	// others — so the admin UI can tell them apart from manual inactivations.
	if got := mustGetExam(t, pool, trialUnstarted).TombstonedAt; got == nil {
		t.Errorf("trial-unstarted: tombstoned_at must be set")
	}
	if got := mustGetExam(t, pool, trialRunning).TombstonedAt; got == nil {
		t.Errorf("trial-running: tombstoned_at must be set (running exam is cut off)")
	}
	if got := mustGetExam(t, pool, futureUnstarted).TombstonedAt; got != nil {
		t.Errorf("future-unstarted: tombstoned_at=%v, want nil (valid account)", got)
	}
	if got := mustGetExam(t, pool, rootUnstarted).TombstonedAt; got != nil {
		t.Errorf("root-unstarted: tombstoned_at=%v, want nil (superadmin)", got)
	}
	// The cut-off running exam is no longer marked as started, so it cannot be
	// resumed and the dashboard shows no phantom running exam.
	if got := mustGetExam(t, pool, trialRunning).ExamStartedAt; got != nil {
		t.Errorf("trial-running: exam_started_at=%v, want cleared (cut off)", got)
	}

	// The pass is idempotent: re-running it leaves the state untouched.
	tombstoneExpiredUsersExamsPass(ctx, pool)
	for _, e := range []struct {
		id   int
		name string
		want string
	}{{trialUnstarted, "trial-unstarted", "inactive"}, {trialRunning, "trial-running", "inactive"}, {futureUnstarted, "future-unstarted", "active"}, {rootUnstarted, "root-unstarted", "active"}} {
		if got := mustGetExam(t, pool, e.id).Status; got != e.want {
			t.Errorf("%s after second pass: status=%s, want %s", e.name, got, e.want)
		}
	}

	// While the account is still expired the rule is CONTINUOUS, not
	// event-driven: an admin re-activating a tombstoned exam (badge click
	// clears the marker) has it re-tombstoned by the next pass — a
	// re-activation only sticks after the account is renewed.
	if _, err := models.ToggleExamStatus(ctx, pool, trialUnstarted); err != nil {
		t.Fatalf("re-activate while still expired: %v", err)
	}
	if re := mustGetExam(t, pool, trialUnstarted); re.Status != "active" || re.TombstonedAt != nil {
		t.Fatalf("re-activate while still expired: status=%s tombstoned_at=%v, want active with nil marker", re.Status, re.TombstonedAt)
	}
	tombstoneExpiredUsersExamsPass(ctx, pool)
	if re := mustGetExam(t, pool, trialUnstarted); re.Status != "inactive" || re.TombstonedAt == nil {
		t.Errorf("re-activated exam of still-expired account after next pass: status=%s tombstoned_at=%v, want re-tombstoned (inactive with marker)", re.Status, re.TombstonedAt)
	}

	// A renewal (admin extends expires_at) un-locks the account but does NOT
	// auto-restore the tombstoned exams — the owner re-activates manually.
	if _, err := pool.Exec(ctx,
		`UPDATE admin_users SET expires_at = now() + interval '7 days' WHERE id = $1`, trial.ID); err != nil {
		t.Fatalf("renew trial account: %v", err)
	}
	if got, msg := models.AuthenticateUser(ctx, pool, "trial1", "pass-trial1"); msg != "" || got == nil {
		t.Errorf("login after renewal: msg=%q user=%+v, want success", msg, got)
	} else if got.IsFeatureLocked() {
		t.Errorf("login after renewal: want IsFeatureLocked()=false, got true")
	}
	if got := mustGetExam(t, pool, trialUnstarted).Status; got != "inactive" {
		t.Errorf("trial-unstarted after renewal: status=%s, want still inactive (no auto-restore)", got)
	}

	// Re-activating the tombstoned exam (the owner clicking the badge after
	// renewal) now sticks — the account is no longer expired, so the next
	// pass leaves it alone: it becomes a normal active exam again.
	if _, err := models.ToggleExamStatus(ctx, pool, trialUnstarted); err != nil {
		t.Fatalf("re-activate trial-unstarted: %v", err)
	}
	if re := mustGetExam(t, pool, trialUnstarted); re.Status != "active" || re.TombstonedAt != nil {
		t.Errorf("trial-unstarted after re-activate: status=%s tombstoned_at=%v, want active with nil marker", re.Status, re.TombstonedAt)
	}
	tombstoneExpiredUsersExamsPass(ctx, pool)
	if re := mustGetExam(t, pool, trialUnstarted); re.Status != "active" || re.TombstonedAt != nil {
		t.Errorf("re-activated exam after renewal + next pass: status=%s tombstoned_at=%v, want still active (re-activation sticks)", re.Status, re.TombstonedAt)
	}
}

// TestExpiredSuspendedAccountSkipsTombstone guards the suspension exception:
// a SUSPENDED account's clock is frozen (its expiry is extended back on
// reactivation), so an in-the-past expires_at while suspended must NOT trigger
// the tombstone — otherwise a reactivated account would find its unpublished
// exams silently deactivated.
func TestExpiredSuspendedAccountSkipsTombstone(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	sub := createOperatorUser(t, pool, "sub1", "SMK 9 Test", "pass-sub1")
	if _, err := pool.Exec(ctx, `
		UPDATE admin_users
		SET status = 'suspended', suspended_at = now() - interval '1 hour',
		    expires_at = now() - interval '1 minute'
		WHERE id = $1`, sub.ID); err != nil {
		t.Fatalf("suspend+expire account: %v", err)
	}
	exam := insertTestExam(t, pool, sub.ID, "suspended-unstarted", nil)

	tombstoneExpiredUsersExamsPass(ctx, pool)
	if got := mustGetExam(t, pool, exam).Status; got != "active" {
		t.Errorf("suspended account's exam after pass: status=%s, want active (clock frozen)", got)
	}
	if got := mustGetExam(t, pool, exam).TombstonedAt; got != nil {
		t.Errorf("suspended account's exam: tombstoned_at=%v, want nil", got)
	}
}
