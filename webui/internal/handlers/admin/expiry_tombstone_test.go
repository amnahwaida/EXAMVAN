package admin

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
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
// time-based), its active exams are auto-inactivated with the tombstone
// marker — mirroring the school policy B. Unstarted exams are tombstoned
// immediately; RUNNING exams are spared within the expiredRunningExamGrace
// window so students mid-exam are not cut off at the expiry instant, and are
// only cut off (exam_started_at cleared) once the account has been expired
// past the grace. The tombstone is never auto-reversed, and a re-activation
// clears the marker. Superadmin accounts and accounts with a future expiry
// are spared. The account itself stays able to log in (feature-locked, not
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

	// A second expired account whose expiry passed LONG ago (beyond the
	// expiredRunningExamGrace window) — used to verify that a running exam is
	// eventually cut off once the grace period has elapsed.
	oldTrial := createOperatorUser(t, pool, "oldtrial", "personal", "pass-oldtrial")
	if _, err := pool.Exec(ctx,
		`UPDATE admin_users SET expires_at = now() - interval '25 hours' WHERE id = $1`, oldTrial.ID); err != nil {
		t.Fatalf("expire old trial account: %v", err)
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
	// tombstoned immediately), a RUNNING one by the same account (within the
	// grace window — must be spared so students mid-exam are not cut off at
	// the expiry instant), a RUNNING one by the long-expired account (past the
	// grace — must be tombstoned, with exam_started_at cleared so the Android
	// app cannot continue it), and unstarted ones by the future-valid account
	// and the superadmin (both must be spared).
	trialUnstarted := insertTestExam(t, pool, trial.ID, "trial-unstarted", nil)
	started := time.Now().UTC().Add(-5 * time.Minute)
	trialRunning := insertTestExam(t, pool, trial.ID, "trial-running", &started)
	oldTrialRunning := insertTestExam(t, pool, oldTrial.ID, "oldtrial-running", &started)
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
	}{{trialUnstarted, "trial-unstarted", "inactive"}, {trialRunning, "trial-running", "active"}, {oldTrialRunning, "oldtrial-running", "inactive"}, {futureUnstarted, "future-unstarted", "active"}, {rootUnstarted, "root-unstarted", "active"}} {
		if got := mustGetExam(t, pool, e.id).Status; got != e.want {
			t.Errorf("%s after pass: status=%s, want %s", e.name, got, e.want)
		}
	}
	// The tombstone marker is set on every auto-inactivated exam — and on no
	// others — so the admin UI can tell them apart from manual inactivations.
	if got := mustGetExam(t, pool, trialUnstarted).TombstonedAt; got == nil {
		t.Errorf("trial-unstarted: tombstoned_at must be set")
	}
	// The running exam within the grace window keeps running (no marker, exam
	// still started) so students are not disconnected mid-test.
	if got := mustGetExam(t, pool, trialRunning).TombstonedAt; got != nil {
		t.Errorf("trial-running (grace): tombstoned_at=%v, want nil (spared by grace)", got)
	}
	if got := mustGetExam(t, pool, trialRunning).ExamStartedAt; got == nil {
		t.Errorf("trial-running (grace): exam_started_at must be kept (exam continues)")
	}
	// The running exam past the grace window is tombstoned and cut off.
	if got := mustGetExam(t, pool, oldTrialRunning).TombstonedAt; got == nil {
		t.Errorf("oldtrial-running: tombstoned_at must be set (running exam past grace is cut off)")
	}
	if got := mustGetExam(t, pool, oldTrialRunning).ExamStartedAt; got != nil {
		t.Errorf("oldtrial-running: exam_started_at=%v, want cleared (cut off)", got)
	}
	if got := mustGetExam(t, pool, futureUnstarted).TombstonedAt; got != nil {
		t.Errorf("future-unstarted: tombstoned_at=%v, want nil (valid account)", got)
	}
	if got := mustGetExam(t, pool, rootUnstarted).TombstonedAt; got != nil {
		t.Errorf("root-unstarted: tombstoned_at=%v, want nil (superadmin)", got)
	}

	// The pass is idempotent: re-running it leaves the state untouched.
	tombstoneExpiredUsersExamsPass(ctx, pool)
	for _, e := range []struct {
		id   int
		name string
		want string
	}{{trialUnstarted, "trial-unstarted", "inactive"}, {trialRunning, "trial-running", "active"}, {oldTrialRunning, "oldtrial-running", "inactive"}, {futureUnstarted, "future-unstarted", "active"}, {rootUnstarted, "root-unstarted", "active"}} {
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

// TestToggleUserStatusLegacyNullExpiryRoundtrip locks in the legacy-account
// fix end-to-end (through the real SQL, not just the pure plan decision): an
// account whose expires_at has never been set (NULL = unlimited) that is
// suspended and then re-activated must come back ACTIVE with expires_at still
// NULL. Before the fix, the reactivation branch granted +1 day to every
// non-active user — silently converting unlimited legacy accounts into
// expiring ones (and tombstoning their exams a day later).
func TestToggleUserStatusLegacyNullExpiryRoundtrip(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	// Legacy account: created without an expiry (expires_at stays NULL).
	legacy := createOperatorUser(t, pool, "legacynull", "SMA 1 Lama", "pass-legacynull")

	// Sanity: the fixture really is a NULL-expiry (unlimited) account.
	if u := mustGetUser(t, pool, "legacynull"); u.ExpiresAt != nil {
		t.Fatalf("fixture: expires_at = %v, want NULL (legacy unlimited account)", u.ExpiresAt)
	}

	// 1) Suspend the active legacy account.
	newStatus, _, err := models.ToggleUserStatus(ctx, pool, legacy.ID)
	if err != nil {
		t.Fatalf("toggle suspend: %v", err)
	}
	if newStatus != models.UserStatusSuspended {
		t.Errorf("after suspend: status=%q, want %q", newStatus, models.UserStatusSuspended)
	}
	u := mustGetUser(t, pool, "legacynull")
	if u.Status != models.UserStatusSuspended {
		t.Errorf("after suspend: db status=%q, want %q", u.Status, models.UserStatusSuspended)
	}
	if u.ExpiresAt != nil {
		t.Errorf("after suspend: expires_at = %v, want NULL (suspend must not touch expiry)", u.ExpiresAt)
	}

	// 2) Re-activate: the account must come back active WITHOUT a +1-day grant.
	newStatus, msg, err := models.ToggleUserStatus(ctx, pool, legacy.ID)
	if err != nil {
		t.Fatalf("toggle reactivate: %v", err)
	}
	if newStatus != models.UserStatusActive {
		t.Errorf("after reactivate: status=%q, want %q", newStatus, models.UserStatusActive)
	}
	if !strings.Contains(msg, "tanpa batas") {
		t.Errorf("after reactivate: msg=%q, want 'tanpa batas' indicator", msg)
	}
	u = mustGetUser(t, pool, "legacynull")
	if u.Status != models.UserStatusActive {
		t.Errorf("after reactivate: db status=%q, want %q", u.Status, models.UserStatusActive)
	}
	// The regression assertion: expires_at must STILL be NULL (unlimited
	// preserved) — the SQL executed by the reactivation must not set it.
	if u.ExpiresAt != nil {
		t.Errorf("after reactivate: expires_at = %v, want NULL — legacy NULL expiry must not be granted a renewal period", u.ExpiresAt)
	}

	// 3) The account behaves as unlimited: not feature-locked, can log in.
	if u.IsFeatureLocked() {
		t.Error("after reactivate: IsFeatureLocked()=true, want false (NULL expiry = unlimited)")
	}
	if got, msg := models.AuthenticateUser(ctx, pool, "legacynull", "pass-legacynull"); msg != "" || got == nil {
		t.Errorf("login after reactivate: msg=%q user=%+v, want success", msg, got)
	}
}

// TestToggleUserStatusLegacyNullRepeatedCyclesNeverDriftsBranch locks in the
// branch-selection stability of planToggleUserStatus for the legacy NULL-expiry
// account across REPEATED suspend/reactivate cycles: every reactivation must
// keep selecting the legacy-NULL branch (expires_at stays NULL, unlimited
// preserved) — never drifting into RenewExpiry (renewal grant) or FreezeClock
// (suspension-duration extension). Branch selection reads Status and ExpiresAt
// only, so as long as expires_at stays NULL the branch cannot drift on its own
// — the regression this guards is a plan reordering that would treat NULL as
// "already expired" (granting a renewal period and silently converting an
// unlimited account into an expiring one). The repeated loop also locks in the execution
// side: each suspend sets suspended_at freshly, and every reactivation must
// clear it, so no stale marker is left behind to double-freeze the clock if
// the account later gains a real expiry.
func TestToggleUserStatusLegacyNullRepeatedCyclesNeverDriftsBranch(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	// Legacy account: created without an expiry (expires_at stays NULL).
	legacy := createOperatorUser(t, pool, "legacycycles", "SMA Siklus", "pass-legacycycles")
	if u := mustGetUser(t, pool, "legacycycles"); u.ExpiresAt != nil {
		t.Fatalf("fixture: expires_at = %v, want NULL (legacy unlimited account)", u.ExpiresAt)
	}

	for cycle := 1; cycle <= 3; cycle++ {
		// --- Suspend: the toggle must take the suspend branch (active →
		// suspended), setting suspended_at so a stale marker could leak into
		// the next reactivation if branch selection ever drifted.
		newStatus, msg, err := models.ToggleUserStatus(ctx, pool, legacy.ID)
		if err != nil {
			t.Fatalf("cycle %d suspend: %v", cycle, err)
		}
		if newStatus != models.UserStatusSuspended {
			t.Errorf("cycle %d after suspend: status=%q, want %q", cycle, newStatus, models.UserStatusSuspended)
		}
		if !strings.Contains(msg, "dinonaktifkan") {
			t.Errorf("cycle %d suspend msg = %q, want the suspend message", cycle, msg)
		}
		u := mustGetUser(t, pool, "legacycycles")
		if u.ExpiresAt != nil {
			t.Errorf("cycle %d after suspend: expires_at = %v, want NULL (suspend must not touch expiry)", cycle, u.ExpiresAt)
		}
		status, cascade, suspendedAt := subFlags(t, pool, legacy.ID)
		if status != models.UserStatusSuspended || suspendedAt == nil {
			t.Errorf("cycle %d after suspend: status=%s suspended_at=%v, want suspended with a set marker", cycle, status, suspendedAt)
		}
		if cascade {
			t.Errorf("cycle %d after suspend: suspended_by_cascade=true, want false (manual toggle, not cascade)", cycle)
		}

		// --- Reactivate: must take the legacy-NULL branch — expires_at stays
		// NULL (unlimited), suspended_at is cleared, and NO +1-day grant or
		// freeze extension message may appear.
		newStatus, msg, err = models.ToggleUserStatus(ctx, pool, legacy.ID)
		if err != nil {
			t.Fatalf("cycle %d reactivate: %v", cycle, err)
		}
		if newStatus != models.UserStatusActive {
			t.Errorf("cycle %d after reactivate: status=%q, want %q", cycle, newStatus, models.UserStatusActive)
		}
		if !strings.Contains(msg, "tanpa batas") {
			t.Errorf("cycle %d reactivate msg = %q, want the legacy-NULL (tanpa batas) message", cycle, msg)
		}
		if strings.Contains(msg, "+14 hari") || strings.Contains(msg, "diperpanjang sampai") {
			t.Errorf("cycle %d reactivate msg = %q, want NO renewal/freeze wording (branch must not drift to RenewExpiry/FreezeClock)", cycle, msg)
		}
		u = mustGetUser(t, pool, "legacycycles")
		if u.ExpiresAt != nil {
			t.Errorf("cycle %d after reactivate: expires_at = %v, want NULL — legacy NULL expiry must survive repeated cycles", cycle, u.ExpiresAt)
		}
		status, cascade, suspendedAt = subFlags(t, pool, legacy.ID)
		if status != models.UserStatusActive || cascade || suspendedAt != nil {
			t.Errorf("cycle %d after reactivate: status=%s cascade=%v suspended_at=%v, want active, no markers (stale freeze must not leak)",
				cycle, status, cascade, suspendedAt)
		}
	}

	// After all cycles the account is still unlimited and fully usable: not
	// feature-locked, can log in.
	u := mustGetUser(t, pool, "legacycycles")
	if u.ExpiresAt != nil {
		t.Errorf("after cycles: expires_at = %v, want NULL", u.ExpiresAt)
	}
	if u.IsFeatureLocked() {
		t.Error("after cycles: IsFeatureLocked()=true, want false (NULL expiry = unlimited)")
	}
	if got, msg := models.AuthenticateUser(ctx, pool, "legacycycles", "pass-legacycycles"); msg != "" || got == nil {
		t.Errorf("login after cycles: msg=%q user=%+v, want success", msg, got)
	}
}

// TestToggleUserStatusPendingOTPBlocked locks in the pending_otp guard: an
// account still awaiting email verification must NOT be (silently) activated
// via the generic suspend/activate toggle — that would bypass the email/OTP
// gate and leave otp_code/otp_expiry dangling. Activation is reserved for the
// OTP confirmation flow and the admin's manual Verify action
// (VerifyUserManual), which both clear the OTP fields.
func TestToggleUserStatusPendingOTPBlocked(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	// Simulate a freshly-registered account with email verification enabled:
	// status pending_otp, OTP fields populated, future expiry.
	u := createOperatorUser(t, pool, "pending1", "personal", "pass-pending1")
	if _, err := pool.Exec(ctx, `
		UPDATE admin_users
		SET status = 'pending_otp', otp_code = '123456',
		    otp_expiry = now() + interval '15 minutes'
		WHERE id = $1`, u.ID); err != nil {
		t.Fatalf("set pending_otp state: %v", err)
	}

	// The toggle must refuse with the sentinel error.
	newStatus, _, err := models.ToggleUserStatus(ctx, pool, u.ID)
	if !errors.Is(err, models.ErrPendingOTPToggleBlocked) {
		t.Fatalf("toggle pending_otp: err=%v, want ErrPendingOTPToggleBlocked (newStatus=%q)", err, newStatus)
	}

	// Nothing may change: status stays pending_otp and the OTP fields survive
	// (so the user can still complete the email confirmation).
	got := mustGetUser(t, pool, "pending1")
	if got.Status != models.UserStatusPendingOTP {
		t.Errorf("status after blocked toggle = %q, want %q (must not be activated)", got.Status, models.UserStatusPendingOTP)
	}
	if got.OTPCode == nil || *got.OTPCode == "" {
		t.Error("otp_code after blocked toggle = nil/empty, want preserved")
	}
	if got.OTPExpiry == nil {
		t.Error("otp_expiry after blocked toggle = nil, want preserved")
	}
}

// TestVerifyUserManualClearsSuspensionMarkers locks in the defensive guard in
// VerifyUserManual: the verify endpoint accepts ANY target status (the UI
// offers it for pending users, but the API does not validate the status — an
// admin may also use it as the escape hatch for a cascade-suspended account).
// Activating a suspended account must therefore clear the suspension markers:
// a dangling suspended_at would make a later reactivation (the EditUser freeze
// block's ResumeSuspendedAccountClock, or the cascade restore) apply a stale
// double-freeze, and a dangling suspended_by_cascade would make a later
// operator return re-suspend the already-verified account.
func TestVerifyUserManualClearsSuspensionMarkers(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	// A user with an ACTIVE redemption, so the test also confirms verify does
	// not corrupt the package clock's alignment with the account expiry.
	createGuruVoucher(t, pool)
	u := createOperatorUser(t, pool, "verify-markers", "SMK Verify Markers", "pass-verify-markers")
	tc := newVoucherTestClient(t, pool)
	tc.login(t, u.ID)
	tc.redeem(t, "IT-GURU")
	u = mustGetUser(t, pool, "verify-markers")
	if u.ExpiresAt == nil {
		t.Fatal("fixture: redeem must have set an account expiry")
	}

	// Pin a suspended state (Go clock, stored verbatim) exactly as a cascade
	// suspension would leave it: locked out 2h ago, future expiry at now+30d.
	goNow := time.Now().UTC()
	pinSuspendedState(t, pool, u.ID, true, goNow.Add(-2*time.Hour), goNow.Add(30*24*time.Hour))
	status, cascade, suspendedAt := subFlags(t, pool, u.ID)
	if status != models.UserStatusSuspended || !cascade || suspendedAt == nil {
		t.Fatalf("fixture: want suspended+cascade+set suspended_at, got status=%s cascade=%v suspendedAt=%v",
			status, cascade, suspendedAt)
	}

	// A superadmin manually verifies the account via the real endpoint (the
	// escape hatch path that accepts a suspended target).
	root, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "rootadmin-verify", Name: "Root Verify",
		PasswordHash: "pass-root-verify", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}
	tc.login(t, root.ID)
	if status, resp := postForm(t, tc.client, tc.srv, "/api/users/"+strconv.Itoa(u.ID)+"/verify", nil); status != http.StatusOK || !resp.Success {
		t.Fatalf("verify user: status=%d resp=%+v", status, resp)
	}

	// The account is active, the OTP fields are cleared, and — the defensive
	// guard under test — the suspension markers are gone.
	u = mustGetUser(t, pool, "verify-markers")
	if u.Status != models.UserStatusActive {
		t.Errorf("status after verify = %q, want active", u.Status)
	}
	if u.OTPCode != nil || u.OTPExpiry != nil {
		t.Errorf("otp fields after verify = %v / %v, want cleared", u.OTPCode, u.OTPExpiry)
	}
	status, cascade, suspendedAt = subFlags(t, pool, u.ID)
	if cascade {
		t.Errorf("suspended_by_cascade after verify = true, want false (dangling marker would re-suspend on cascade restore)")
	}
	if suspendedAt != nil {
		t.Errorf("suspended_at after verify = %v, want NULL (dangling marker would double-freeze on next reactivation)", suspendedAt)
	}

	// The account is usable again: the (previously suspended) owner can
	// authenticate — the end-to-end outcome of the manual verification.
	if _, msg := models.AuthenticateUser(ctx, pool, "verify-markers", "pass-verify-markers"); msg != "" {
		t.Errorf("login after verify: msg=%q, want success", msg)
	}

	// The regression assertion: the freeze helper behind the toggle/EditUser
	// reactivation paths must be a NO-OP after the verified activation — a
	// dangling suspended_at (pre-guard) would extend expires_at by the full
	// stale 2h suspension a second time (double-freeze).
	extended, err := models.ResumeSuspendedAccountClock(ctx, pool, u.ID)
	if err != nil {
		t.Fatalf("resume suspended account clock: %v", err)
	}
	if extended != nil {
		t.Errorf("ResumeSuspendedAccountClock after verify = %v, want nil (no stale freeze: markers were cleared)", extended)
	}

	// The active package clock stays aligned with the (unchanged) account
	// expiry: verify must not have realigned or corrupted it. Both derive from
	// the same expires_at, so the DB-vs-Go tolerance mirrors the sibling tests.
	var remaining int64
	if err := pool.QueryRow(ctx,
		`SELECT remaining_seconds FROM voucher_redemptions WHERE user_id=$1 AND is_active`,
		u.ID).Scan(&remaining); err != nil {
		t.Fatalf("load redemption after verify: %v", err)
	}
	want := int64(u.ExpiresAt.Sub(time.Now().UTC()) / time.Second)
	const tol = 120
	if d := remaining - want; d < -tol || d > tol {
		t.Errorf("redemption remaining=%d vs account expires_at−now=%d, want within ±%ds", remaining, want, tol)
	}
}

// TestVerifyUserManualKeepsActiveRedemptionAligned locks in the redemption
// side of the VerifyUserManual reactivation path: verifying a SUSPENDED user
// who holds an ACTIVE package must leave the package clock untouched and
// EXACTLY aligned with the (unchanged) account expiry. Verify flips the
// status and clears the suspension markers, but it must NOT realign, restart,
// or zero the active redemption — there is no new expiry to align to, and the
// package clock was frozen during the suspension anyway. The assertion is the
// same DB-internal exact invariant as the freeze tests: the stored
// remaining_seconds must equal (expires_at − activated_at), both pinned
// consistently before the verify and untouched by it.
func TestVerifyUserManualKeepsActiveRedemptionAligned(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	// A user with an ACTIVE package redemption.
	createGuruVoucher(t, pool)
	u := createOperatorUser(t, pool, "verify-align", "SMK Verify Align", "pass-verify-align")
	tc := newVoucherTestClient(t, pool)
	tc.login(t, u.ID)
	tc.redeem(t, "IT-GURU")
	u = mustGetUser(t, pool, "verify-align")
	if u.ExpiresAt == nil {
		t.Fatal("fixture: redeem must have set an account expiry")
	}

	// Pin the account AND the redemption clock consistently (Go clock,
	// stored verbatim): suspended 2h ago with a future expiry at now+30d, and
	// the active redemption's clock pinned to the same instant — activated_at
	// at the suspension start and remaining_seconds = 30d + 2h, so
	// expires_at − activated_at == remaining_seconds holds exactly and stays
	// exact after verify (which must touch neither side).
	goNow := time.Now().UTC()
	pinSuspendedState(t, pool, u.ID, true, goNow.Add(-2*time.Hour), goNow.Add(30*24*time.Hour))
	if _, err := pool.Exec(ctx, `
		UPDATE voucher_redemptions
		SET activated_at = $2, remaining_seconds = $3
		WHERE user_id = $1 AND is_active`,
		u.ID, goNow.Add(-2*time.Hour), int64(30*24*time.Hour/time.Second+2*time.Hour/time.Second)); err != nil {
		t.Fatalf("pin redemption clock: %v", err)
	}

	var expiresBefore time.Time
	if err := pool.QueryRow(ctx,
		`SELECT expires_at FROM admin_users WHERE id = $1`, u.ID).Scan(&expiresBefore); err != nil {
		t.Fatalf("read expires_at: %v", err)
	}
	var remainingBefore int64
	var activatedBefore time.Time
	if err := pool.QueryRow(ctx,
		`SELECT remaining_seconds, activated_at FROM voucher_redemptions WHERE user_id=$1 AND is_active`,
		u.ID).Scan(&remainingBefore, &activatedBefore); err != nil {
		t.Fatalf("load active redemption: %v", err)
	}

	// A superadmin verifies the suspended account via the real endpoint.
	root, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "rootadmin-align", Name: "Root Align",
		PasswordHash: "pass-root-align", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}
	tc.login(t, root.ID)
	if status, resp := postForm(t, tc.client, tc.srv, "/api/users/"+strconv.Itoa(u.ID)+"/verify", nil); status != http.StatusOK || !resp.Success {
		t.Fatalf("verify user: status=%d resp=%+v", status, resp)
	}

	got := mustGetUser(t, pool, "verify-align")
	if got.Status != models.UserStatusActive {
		t.Fatalf("status after verify = %q, want active", got.Status)
	}
	if got.ExpiresAt == nil || !approxEqual(*got.ExpiresAt, expiresBefore, time.Second) {
		t.Errorf("expires_at after verify = %v, want unchanged ≈ %v (verify is not a freeze)", got.ExpiresAt, expiresBefore)
	}
	var remainingAfter int64
	var activatedAfter time.Time
	if err := pool.QueryRow(ctx,
		`SELECT remaining_seconds, activated_at FROM voucher_redemptions WHERE user_id=$1 AND is_active`,
		u.ID).Scan(&remainingAfter, &activatedAfter); err != nil {
		t.Fatalf("load redemption after verify: %v", err)
	}

	// Verify must not touch the package clock: no realign (there is no new
	// expiry), no restart, no zeroing.
	if remainingAfter != remainingBefore {
		t.Errorf("remaining_seconds after verify = %d, want unchanged %d (verify must not realign)",
			remainingAfter, remainingBefore)
	}
	if !activatedAfter.Equal(activatedBefore) {
		t.Errorf("activated_at after verify = %v, want unchanged %v (verify must not restart the clock)",
			activatedAfter, activatedBefore)
	}

	// The exact DB-internal invariant still holds after the reactivation: the
	// stored remaining_seconds equals (expires_at − activated_at), both
	// untouched by verify (float8→bigint cast rounds to nearest, so
	// [want, want+1] is allowed — mirroring assertFrozenRedemptionSync).
	want := int64(got.ExpiresAt.Sub(activatedAfter) / time.Second)
	if remainingAfter < want || remainingAfter > want+1 {
		t.Errorf("remaining_seconds after verify = %d, want %d or %d (= expires_at − activated_at: the clock stayed aligned)",
			remainingAfter, want, want+1)
	}

	// The reactivated owner can log in again.
	if _, msg := models.AuthenticateUser(ctx, pool, "verify-align", "pass-verify-align"); msg != "" {
		t.Errorf("login after verify: msg=%q, want success", msg)
	}
}

// TestToggleUserStatusFreezeClockExtendsExpiry asserts the exact freeze-clock
// arithmetic end-to-end: reactivating a suspended account that still has a
// valid future expiry must move expires_at forward by (approximately) the
// suspension duration — the package clock did not burn while the account was
// locked out — not merely flip the status back to active. This locks in the
// value computed by computeFrozenExpiry through the real SQL.
func TestToggleUserStatusFreezeClockExtendsExpiry(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	u := createOperatorUser(t, pool, "freeze1", "SMK Freeze", "pass-freeze1")

	// Deterministic suspension state: suspended 2 hours ago with a valid future
	// expiry (7 days out). Both stamps are generated on the Go clock and stored
	// verbatim, so the freeze arithmetic inside ResumeSuspendedAccountClock
	// (now.Sub(suspended_at), also on the Go clock) is measured on a single
	// clock — immune to any host/container clock skew — and only the
	// sub-second gap between this setup and the reactivation matters.
	goNow := time.Now().UTC()
	pinSuspendedState(t, pool, u.ID, false, goNow.Add(-2*time.Hour), goNow.Add(7*24*time.Hour))

	var expiresBefore time.Time
	if err := pool.QueryRow(ctx,
		`SELECT expires_at FROM admin_users WHERE id = $1`, u.ID).Scan(&expiresBefore); err != nil {
		t.Fatalf("read suspended state: %v", err)
	}

	newStatus, msg, err := models.ToggleUserStatus(ctx, pool, u.ID)
	if err != nil {
		t.Fatalf("toggle reactivate: %v", err)
	}
	if newStatus != models.UserStatusActive {
		t.Fatalf("newStatus = %q, want %q", newStatus, models.UserStatusActive)
	}
	if !strings.Contains(msg, "diperpanjang sampai") {
		t.Errorf("msg = %q, want the freeze-clock extension message", msg)
	}

	got := mustGetUser(t, pool, "freeze1")
	if got.Status != models.UserStatusActive {
		t.Fatalf("db status = %q, want %q", got.Status, models.UserStatusActive)
	}
	if got.ExpiresAt == nil {
		t.Fatal("expires_at = nil after freeze reactivation, want extended value")
	}

	// The exact assertion: expires_at must move forward by ~the suspension
	// duration (2h) — shared freeze-clock arithmetic (assertFrozenExpiryDelta).
	assertFrozenExpiryDelta(t, "freeze1", expiresBefore, *got.ExpiresAt)

	// The freeze is not just a status flip: expiry really moved forward, and
	// the account still has a future expiry (not a +1-day renewal).
	if !got.ExpiresAt.After(expiresBefore) {
		t.Errorf("expires_at = %v, want after %v (must not regress)", got.ExpiresAt, expiresBefore)
	}
	if got.ExpiresAt.Before(time.Now().UTC()) {
		t.Errorf("expires_at = %v, want still in the future", got.ExpiresAt)
	}

	// The suspension marker is cleared so a second reactivation is a no-op
	// freeze, not a double extension.
	var suspendedAfter *time.Time
	if err := pool.QueryRow(ctx,
		`SELECT suspended_at FROM admin_users WHERE id = $1`, u.ID).Scan(&suspendedAfter); err != nil {
		t.Fatalf("read suspended_at after reactivation: %v", err)
	}
	if suspendedAfter != nil {
		t.Errorf("suspended_at after reactivation = %v, want NULL", suspendedAfter)
	}
}

// TestToggleUserStatusFreezeClockSyncsRedemptionRemaining locks in the OTHER
// half of the freeze clock: reactivating a suspended user must not only move
// expires_at forward by the suspension duration (TestToggleUserStatusFreeze-
// ClockExtendsExpiry) — the ACTIVE package's clock must follow, with
// remaining_seconds rewritten to the frozen account expiry and activated_at
// restarted. The key assertion is DB-internal and EXACT: the sync statement
// writes remaining_seconds and activated_at together, so the stored
// remaining_seconds must equal (new expires_at − activated_at) — a sync that
// is skipped, or realigned to the wrong expiry, fails the equality by the
// whole suspension duration (~2h).
func TestToggleUserStatusFreezeClockSyncsRedemptionRemaining(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	// A user with an ACTIVE package redemption whose clock must follow the
	// frozen account expiry on reactivation.
	createGuruVoucher(t, pool)
	u := createOperatorUser(t, pool, "freeze-red", "SMK Freeze Red", "pass-freeze-red")
	tc := newVoucherTestClient(t, pool)
	tc.login(t, u.ID)
	tc.redeem(t, "IT-GURU")
	u = mustGetUser(t, pool, "freeze-red")
	if u.ExpiresAt == nil {
		t.Fatal("fixture: redeem must have set an account expiry")
	}

	// Deterministic suspension state (Go clock, stored verbatim): suspended 2
	// hours ago with a valid future expiry pinned to now + 30d. Both stamps
	// are on the Go clock so the freeze arithmetic (now.Sub(suspended_at))
	// is single-clock; only the sub-second setup-to-reactivate gap matters.
	goNow := time.Now().UTC()
	pinSuspendedState(t, pool, u.ID, false, goNow.Add(-2*time.Hour), goNow.Add(30*24*time.Hour))

	var expiresBefore time.Time
	if err := pool.QueryRow(ctx,
		`SELECT expires_at FROM admin_users WHERE id = $1`, u.ID).Scan(&expiresBefore); err != nil {
		t.Fatalf("read expires_at before reactivation: %v", err)
	}
	var redemptionID int
	var remainingBefore int64
	var activatedBefore time.Time
	if err := pool.QueryRow(ctx,
		`SELECT id, remaining_seconds, activated_at FROM voucher_redemptions WHERE user_id=$1 AND is_active`,
		u.ID).Scan(&redemptionID, &remainingBefore, &activatedBefore); err != nil {
		t.Fatalf("load active redemption: %v", err)
	}

	// Reactivate via the toggle: the FreezeClock branch extends expires_at by
	// the suspension duration AND realigns the active package clock.
	newStatus, msg, err := models.ToggleUserStatus(ctx, pool, u.ID)
	if err != nil {
		t.Fatalf("toggle reactivate: %v", err)
	}
	if newStatus != models.UserStatusActive {
		t.Fatalf("newStatus = %q, want %q", newStatus, models.UserStatusActive)
	}
	if !strings.Contains(msg, "diperpanjang sampai") {
		t.Errorf("msg = %q, want the freeze-clock extension message", msg)
	}

	// Post-reactivation state.
	got := mustGetUser(t, pool, "freeze-red")
	var remainingAfter int64
	var activatedAfter time.Time
	if err := pool.QueryRow(ctx,
		`SELECT remaining_seconds, activated_at FROM voucher_redemptions WHERE id=$1`,
		redemptionID).Scan(&remainingAfter, &activatedAfter); err != nil {
		t.Fatalf("load redemption after reactivation: %v", err)
	}
	assertFrozenRedemptionSync(t, "freeze-red", expiresBefore, remainingBefore, activatedBefore, got.ExpiresAt, remainingAfter, activatedAfter)
}

// TestToggleOperatorRestoreFreezeClockSyncsSubRedemption locks in the cascade
// restore realignment (RestoreCascadeSuspendedInstansi): reactivating a school
// operator via toggle-status restores every cascade-suspended sub-account with
// a clock freeze — and a sub that runs its OWN active package must have its
// remaining_seconds rewritten to the frozen account expiry, exactly like the
// single-user freeze path (TestToggleUserStatusFreezeClockSyncsRedemption-
// Remaining). The assertion is the same exact DB-internal invariant: the
// stored remaining_seconds must equal (new expires_at − activated_at), both
// written together by the shared sync statement.
func TestToggleOperatorRestoreFreezeClockSyncsSubRedemption(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucher(t, pool)
	createGuruVoucherCode(t, pool, "IT-GURU-SUB") // the sub's own active package
	op := createOperatorUser(t, pool, "op-cas", "SMK Cascade", "pass-op-cas")

	tc := newVoucherTestClient(t, pool)
	tc.login(t, op.ID)
	tc.redeem(t, "IT-SEKOLAH")
	op = mustGetUser(t, pool, "op-cas")
	if !models.HasRole(op.Role, models.RoleOperator) {
		t.Fatalf("op must hold the operator role after redeeming the school voucher")
	}
	if status, resp := tc.createUser(t, "guru2"); status != http.StatusOK || !resp.Success {
		t.Fatalf("create guru2: status=%d resp=%+v", status, resp)
	}
	guru2 := mustGetUser(t, pool, "guru2")
	tc.login(t, guru2.ID)
	tc.redeem(t, "IT-GURU-SUB")
	if _, msg := models.AuthenticateUser(ctx, pool, "guru2", "pass-guru2"); msg != "" {
		t.Fatalf("guru2 login after own redeem: msg=%q, want success", msg)
	}

	// A superadmin suspends the operator → the cascade suspends the sub
	// (suspended_by_cascade, suspended_at = now()).
	root, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "rootadmin-cas", Name: "Root Cas",
		PasswordHash: "pass-root-cas", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}
	tc.login(t, root.ID)
	if status, resp := postForm(t, tc.client, tc.srv, "/api/users/"+strconv.Itoa(op.ID)+"/toggle-status", nil); status != http.StatusOK || !resp.Success {
		t.Fatalf("toggle suspend op: status=%d resp=%+v", status, resp)
	}
	guru2 = mustGetUser(t, pool, "guru2")
	if status, cascade, _ := subFlags(t, pool, guru2.ID); status != models.UserStatusSuspended || !cascade {
		t.Fatalf("guru2 must be cascade-suspended, got status=%s cascade=%v", status, cascade)
	}

	// Pin the sub's clock deterministically (Go clock, verbatim): suspended 2
	// hours ago with a future expiry at now + 30d — the freeze arithmetic
	// (now - suspended_at) inside RestoreCascadeSuspendedInstansi is
	// single-clock. The cascade marker stays set.
	goNow := time.Now().UTC()
	pinSuspendedClock(t, pool, guru2.ID, goNow.Add(-2*time.Hour), goNow.Add(30*24*time.Hour))
	var expiresBefore time.Time
	if err := pool.QueryRow(ctx,
		`SELECT expires_at FROM admin_users WHERE id = $1`, guru2.ID).Scan(&expiresBefore); err != nil {
		t.Fatalf("read sub expires_at: %v", err)
	}
	var redemptionID int
	var remainingBefore int64
	var activatedBefore time.Time
	if err := pool.QueryRow(ctx,
		`SELECT id, remaining_seconds, activated_at FROM voucher_redemptions WHERE user_id=$1 AND is_active`,
		guru2.ID).Scan(&redemptionID, &remainingBefore, &activatedBefore); err != nil {
		t.Fatalf("load sub redemption: %v", err)
	}

	// Reactivating the operator triggers the cascade restore: the shared
	// RestoreCascadeSuspendedInstansi freezes the sub's expiry and realigns
	// its active package clock.
	if status, resp := postForm(t, tc.client, tc.srv, "/api/users/"+strconv.Itoa(op.ID)+"/toggle-status", nil); status != http.StatusOK || !resp.Success {
		t.Fatalf("toggle reactivate op: status=%d resp=%+v", status, resp)
	}

	got := mustGetUser(t, pool, "guru2")
	status, cascade, suspendedAt := subFlags(t, pool, guru2.ID)
	if status != models.UserStatusActive {
		t.Errorf("guru2 after restore: status=%s, want active", status)
	}
	if cascade {
		t.Errorf("guru2 after restore: suspended_by_cascade=true, want false")
	}
	if suspendedAt != nil {
		t.Errorf("guru2 after restore: suspended_at=%v, want NULL", suspendedAt)
	}
	if _, msg := models.AuthenticateUser(ctx, pool, "guru2", "pass-guru2"); msg != "" {
		t.Errorf("login guru2 after restore: msg=%q, want success", msg)
	}
	var remainingAfter int64
	var activatedAfter time.Time
	if err := pool.QueryRow(ctx,
		`SELECT remaining_seconds, activated_at FROM voucher_redemptions WHERE id=$1`,
		redemptionID).Scan(&remainingAfter, &activatedAfter); err != nil {
		t.Fatalf("load sub redemption after restore: %v", err)
	}
	assertFrozenRedemptionSync(t, "guru2 after toggle restore", expiresBefore, remainingBefore, activatedBefore, got.ExpiresAt, remainingAfter, activatedAfter)
}

// TestEditUserReactivationShowsFreezeMessage covers the EditUser reactivation
// path for a suspended account whose expiry has ALREADY passed (suspended_at <
// now, expires_at < now). The suspension-clock freeze pushes expires_at back
// into the future (resurrecting the account by the suspension duration), and
// the success message must now surface the extended expiry ("Masa aktif
// diperpanjang sampai ...") — previously the freeze ran silently and the
// admin only saw "Pengaturan user berhasil diperbarui".
func TestEditUserReactivationShowsFreezeMessage(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	u := createOperatorUser(t, pool, "editfreeze1", "SMK Edit Freeze", "pass-editfreeze1")

	// Suspended 2 hours ago (Go clock) with an expiry that already passed 1
	// hour ago: the freeze must resurrect it to ~1 hour in the future.
	goNow := time.Now().UTC()
	pinSuspendedState(t, pool, u.ID, false, goNow.Add(-2*time.Hour), goNow.Add(-time.Hour))

	var expiresBefore time.Time
	if err := pool.QueryRow(ctx,
		`SELECT expires_at FROM admin_users WHERE id = $1`, u.ID).Scan(&expiresBefore); err != nil {
		t.Fatalf("read expires_at: %v", err)
	}

	// A superadmin reactivates via the edit form: status=active, no explicit
	// expiry (the path that used to freeze the clock silently).
	root, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "rootadmin-edit", Name: "Root Edit",
		PasswordHash: "pass-root-edit", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}
	tc := newVoucherTestClient(t, pool)
	tc.login(t, root.ID)

	status, resp := postJSON(t, tc.client, tc.srv, "/api/users/"+strconv.Itoa(u.ID)+"/edit",
		map[string]interface{}{"status": models.UserStatusActive})
	if status != http.StatusOK || !resp.Success {
		t.Fatalf("edit reactivate: status=%d resp=%+v", status, resp)
	}
	// The freeze message must appear (the previous behavior only confirmed
	// the edit generically).
	if !strings.Contains(resp.Message, "diperpanjang sampai") {
		t.Errorf("edit message = %q, want 'Masa aktif diperpanjang sampai ...'", resp.Message)
	}

	got := mustGetUser(t, pool, "editfreeze1")
	if got.Status != models.UserStatusActive {
		t.Errorf("status after edit reactivation = %q, want active", got.Status)
	}
	if got.ExpiresAt == nil {
		t.Fatal("expires_at = nil after edit reactivation, want resurrected value")
	}
	// The account is resurrected: expired 1h ago, frozen by 2h of suspension
	// → now ~1h in the future (delta verified by assertFrozenExpiryDelta).
	assertFrozenExpiryDelta(t, "editfreeze1", expiresBefore, *got.ExpiresAt)
	if !got.ExpiresAt.After(time.Now().UTC()) {
		t.Errorf("expires_at = %v, want back in the future (resurrected)", got.ExpiresAt)
	}

	// The suspension marker is cleared.
	var suspendedAfter *time.Time
	if err := pool.QueryRow(ctx,
		`SELECT suspended_at FROM admin_users WHERE id = $1`, u.ID).Scan(&suspendedAfter); err != nil {
		t.Fatalf("read suspended_at after edit: %v", err)
	}
	if suspendedAfter != nil {
		t.Errorf("suspended_at after edit reactivation = %v, want NULL", suspendedAfter)
	}
}

// TestEditUserReactivationFreezeClockSyncsRedemption asserts the redemption
// side of the EditUser reactivation freeze: reactivating a suspended user via
// the edit form (status=active, no explicit expiry) must not only surface the
// "diperpanjang sampai" message (TestEditUserReactivationShowsFreezeMessage) —
// the ACTIVE package's clock must be realigned to the frozen account expiry,
// exactly like the toggle path (TestToggleUserStatusFreezeClockSyncsRedemption-
// Remaining). Same exact DB-internal invariant: the stored remaining_seconds
// must equal (new expires_at − activated_at), both written together by the
// shared sync statement.
func TestEditUserReactivationFreezeClockSyncsRedemption(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	// A user with an ACTIVE package redemption whose clock must follow the
	// frozen account expiry on edit-form reactivation.
	createGuruVoucher(t, pool)
	u := createOperatorUser(t, pool, "edit-freeze-red", "SMK Edit Freeze Red", "pass-edit-freeze-red")
	tc := newVoucherTestClient(t, pool)
	tc.login(t, u.ID)
	tc.redeem(t, "IT-GURU")
	u = mustGetUser(t, pool, "edit-freeze-red")
	if u.ExpiresAt == nil {
		t.Fatal("fixture: redeem must have set an account expiry")
	}

	// Deterministic suspension state (Go clock, stored verbatim): suspended 2
	// hours ago with a valid future expiry pinned to now + 30d.
	goNow := time.Now().UTC()
	pinSuspendedState(t, pool, u.ID, false, goNow.Add(-2*time.Hour), goNow.Add(30*24*time.Hour))

	var expiresBefore time.Time
	if err := pool.QueryRow(ctx,
		`SELECT expires_at FROM admin_users WHERE id = $1`, u.ID).Scan(&expiresBefore); err != nil {
		t.Fatalf("read expires_at before reactivation: %v", err)
	}
	var redemptionID int
	var remainingBefore int64
	var activatedBefore time.Time
	if err := pool.QueryRow(ctx,
		`SELECT id, remaining_seconds, activated_at FROM voucher_redemptions WHERE user_id=$1 AND is_active`,
		u.ID).Scan(&redemptionID, &remainingBefore, &activatedBefore); err != nil {
		t.Fatalf("load active redemption: %v", err)
	}

	// A superadmin reactivates via the edit form: status=active, no explicit
	// expiry → the freeze block runs ResumeSuspendedAccountClock.
	root, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "rootadmin-edit-red", Name: "Root Edit Red",
		PasswordHash: "pass-root-edit-red", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}
	tc.login(t, root.ID)
	status, resp := postJSON(t, tc.client, tc.srv, "/api/users/"+strconv.Itoa(u.ID)+"/edit",
		map[string]interface{}{"status": models.UserStatusActive})
	if status != http.StatusOK || !resp.Success {
		t.Fatalf("edit reactivate: status=%d resp=%+v", status, resp)
	}
	if !strings.Contains(resp.Message, "diperpanjang sampai") {
		t.Errorf("edit message = %q, want 'Masa aktif diperpanjang sampai ...'", resp.Message)
	}

	// Post-reactivation state.
	got := mustGetUser(t, pool, "edit-freeze-red")
	if got.Status != models.UserStatusActive {
		t.Errorf("status after edit reactivation = %q, want active", got.Status)
	}
	var suspendedAfter *time.Time
	if err := pool.QueryRow(ctx,
		`SELECT suspended_at FROM admin_users WHERE id = $1`, u.ID).Scan(&suspendedAfter); err != nil {
		t.Fatalf("read suspended_at after edit: %v", err)
	}
	if suspendedAfter != nil {
		t.Errorf("suspended_at after edit reactivation = %v, want NULL", suspendedAfter)
	}
	var remainingAfter int64
	var activatedAfter time.Time
	if err := pool.QueryRow(ctx,
		`SELECT remaining_seconds, activated_at FROM voucher_redemptions WHERE id=$1`,
		redemptionID).Scan(&remainingAfter, &activatedAfter); err != nil {
		t.Fatalf("load redemption after reactivation: %v", err)
	}
	assertFrozenRedemptionSync(t, "edit-freeze-red", expiresBefore, remainingBefore, activatedBefore, got.ExpiresAt, remainingAfter, activatedAfter)
	if _, msg := models.AuthenticateUser(ctx, pool, "edit-freeze-red", "pass-edit-freeze-red"); msg != "" {
		t.Errorf("login after edit reactivation: msg=%q, want success", msg)
	}
}

// TestBillingDisplayShowsFrozenRemainingAfterFreezeClock locks in what the
// billing page shows after a freeze clock: the ACTIVE package's "Sisa Masa
// Aktif" is derived from the (frozen) account expires_at — the authoritative
// clock — so the suspension duration is added back into the displayed
// remaining lifetime, and the displayed value agrees with the realigned
// remaining_seconds written by the freeze sync (a DB-internal exact
// invariant). Drives the real /admin/api/vouchers/mine endpoint (wired into
// the test router) rather than the billing-ping probe.
func TestBillingDisplayShowsFrozenRemainingAfterFreezeClock(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	// A user with an ACTIVE package redemption whose remaining lifetime must
	// be shown correctly (with the suspension added back) after reactivation.
	createGuruVoucher(t, pool)
	u := createOperatorUser(t, pool, "bill-freeze", "SMK Bill Freeze", "pass-bill-freeze")
	tc := newVoucherTestClient(t, pool)
	tc.login(t, u.ID)
	tc.redeem(t, "IT-GURU")
	u = mustGetUser(t, pool, "bill-freeze")
	if u.ExpiresAt == nil {
		t.Fatal("fixture: redeem must have set an account expiry")
	}

	// Deterministic suspension state (Go clock, stored verbatim): suspended 2
	// hours ago with a valid future expiry pinned to now + 30d.
	goNow := time.Now().UTC()
	pinSuspendedState(t, pool, u.ID, false, goNow.Add(-2*time.Hour), goNow.Add(30*24*time.Hour))

	// Reactivate via the toggle: the FreezeClock branch extends expires_at by
	// the suspension duration (2h) and realigns the active package clock.
	newStatus, _, err := models.ToggleUserStatus(ctx, pool, u.ID)
	if err != nil {
		t.Fatalf("toggle reactivate: %v", err)
	}
	if newStatus != models.UserStatusActive {
		t.Fatalf("newStatus = %q, want %q", newStatus, models.UserStatusActive)
	}

	// The frozen account expiry is the authoritative clock the display derives
	// from, and the realigned stored remaining_seconds must agree with it. The
	// identity fields (id, package) are read too so the rendered item can be
	// matched against the very redemption the display is built from.
	got := mustGetUser(t, pool, "bill-freeze")
	if got.ExpiresAt == nil {
		t.Fatal("expires_at = nil after freeze reactivation")
	}
	var redemptionID int
	var redemptionPackage string
	var storedRemaining int64
	if err := pool.QueryRow(ctx,
		`SELECT id, package, remaining_seconds FROM voucher_redemptions WHERE user_id=$1 AND is_active`,
		u.ID).Scan(&redemptionID, &redemptionPackage, &storedRemaining); err != nil {
		t.Fatalf("load realigned redemption: %v", err)
	}

	// Drive the real billing display endpoint as the reactivated user and
	// parse the ACTIVE package item exactly as billing.html would render it.
	tc.login(t, u.ID) // refresh the session: AuthRequired clears it on suspend
	payload, activeIdx := fetchBillingDisplay(t, tc)
	active := assertBillingDisplay(t, "bill-freeze", "freeze re-opened the clock",
		*got.ExpiresAt, storedRemaining, redemptionID, redemptionPackage, payload.Redemptions[activeIdx])

	// Sanity: the displayed lifetime is strictly more than the pre-freeze ~30d
	// (the suspension was added back, not burned).
	if active.RemainingSeconds <= 30*86400 {
		t.Errorf("display remaining_seconds=%d, want > %d (30d + 2h suspension added back)",
			active.RemainingSeconds, 30*86400)
	}
}

// TestBillingDisplayShowsRenewedRemainingAfterExpiryRenewal locks in what the
// billing page shows after the RenewExpiry branch of ToggleUserStatus: an
// EXPIRED suspended account (expires_at in the past) reactivated by the
// toggle is granted a fresh renewal period (default_active_days = 14 days) —
// the renewal SUPERSEDES the suspension freeze — and the ACTIVE package's
// clock is realigned to the renewed expiry (SyncActiveRedemptionToExpiry).
// The billing display (derived from the account expires_at) must therefore
// show ~14 days, NOT the stale pre-renewal ~30d remaining_seconds, and must
// agree with the realigned stored clock. Drives the real
// /admin/api/vouchers/mine endpoint.
func TestBillingDisplayShowsRenewedRemainingAfterExpiryRenewal(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	// A user with an ACTIVE package redemption whose clock must be realigned
	// to the renewed expiry on reactivation.
	createGuruVoucher(t, pool)
	u := createOperatorUser(t, pool, "bill-renew", "SMK Bill Renew", "pass-bill-renew")
	tc := newVoucherTestClient(t, pool)
	tc.login(t, u.ID)
	tc.redeem(t, "IT-GURU")
	u = mustGetUser(t, pool, "bill-renew")
	if u.ExpiresAt == nil {
		t.Fatal("fixture: redeem must have set an account expiry")
	}

	// Deterministic suspended+EXPIRED state (Go clock, stored verbatim):
	// suspended 2h ago with the expiry ALREADY in the past (1h ago) — the
	// exact condition that selects the RenewExpiry (renewal) branch, not the
	// FreezeClock branch.
	goNow := time.Now().UTC()
	pinSuspendedState(t, pool, u.ID, false, goNow.Add(-2*time.Hour), goNow.Add(-time.Hour))
	var storedBefore int64
	if err := pool.QueryRow(ctx,
		`SELECT remaining_seconds FROM voucher_redemptions WHERE user_id=$1 AND is_active`,
		u.ID).Scan(&storedBefore); err != nil {
		t.Fatalf("load pre-renewal remaining_seconds: %v", err)
	}
	if storedBefore < 20*86400 {
		t.Fatalf("fixture: pre-renewal remaining=%d, want ~30 days (the stale clock the renewal must supersede)", storedBefore)
	}

	// Reactivate via the toggle: the RenewExpiry branch grants a fresh renewal
	// period (now + default_active_days) and realigns the active package clock
	// to it.
	newStatus, msg, err := models.ToggleUserStatus(ctx, pool, u.ID)
	if err != nil {
		t.Fatalf("toggle reactivate: %v", err)
	}
	if newStatus != models.UserStatusActive {
		t.Fatalf("newStatus = %q, want %q", newStatus, models.UserStatusActive)
	}
	if !strings.Contains(msg, "+14 hari") {
		t.Errorf("msg = %q, want the +14-day renewal indicator", msg)
	}

	// The renewed account expiry is the authoritative clock the display
	// derives from, and the realigned stored clock must agree with it.
	got := mustGetUser(t, pool, "bill-renew")
	if got.ExpiresAt == nil {
		t.Fatal("expires_at = nil after renewal reactivation")
	}
	// Sanity: the renewal really granted ~14 days from now (not a freeze of the
	// stale past expiry).
	if got.ExpiresAt.Before(time.Now().UTC().Add(13 * 24 * time.Hour)) {
		t.Errorf("renewed expires_at = %v, want ~now + 14 days", got.ExpiresAt)
	}
	var redemptionID int
	var redemptionPackage string
	var storedRemaining int64
	if err := pool.QueryRow(ctx,
		`SELECT id, package, remaining_seconds FROM voucher_redemptions WHERE user_id=$1 AND is_active`,
		u.ID).Scan(&redemptionID, &redemptionPackage, &storedRemaining); err != nil {
		t.Fatalf("load realigned redemption: %v", err)
	}

	// Drive the real billing display endpoint as the reactivated user.
	tc.login(t, u.ID)
	payload, activeIdx := fetchBillingDisplay(t, tc)
	active := assertBillingDisplay(t, "bill-renew", "renewal re-opened the clock",
		*got.ExpiresAt, storedRemaining, redemptionID, redemptionPackage, payload.Redemptions[activeIdx])

	// The renewal visibly replaced the stale clock: the displayed lifetime is
	// ~14 days — far below the pre-renewal ~30d.
	if active.RemainingSeconds > 15*86400 {
		t.Errorf("display remaining_seconds=%d, want ~14 days after renewal (stale %d must be superseded)",
			active.RemainingSeconds, storedBefore)
	}
}

// TestEditUserOperatorExpiryCascadeSyncsInstansiRedemptions locks in the
// shared instansi-wide redemption realignment (SyncInstansiActiveRedemptions-
// ToExpiry) through real SQL: when a superadmin rewrites an operator's expiry,
// every instansi account's expires_at follows the operator's new expiry AND
// their ACTIVE package clocks are rewritten to match — so the billing display
// and later pause computations never disagree with the account state. This is
// the only call site of the shared helper not already covered by the voucher
// switch / toggle-status restore tests.
func TestEditUserOperatorExpiryCascadeSyncsInstansiRedemptions(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucher(t, pool)
	createGuruVoucherCode(t, pool, "IT-GURU-SUB") // the sub's own active package
	op := createOperatorUser(t, pool, "op-exp", "SMK Exp Sync", "pass-op-exp")

	tc := newVoucherTestClient(t, pool)
	tc.login(t, op.ID)
	tc.redeem(t, "IT-SEKOLAH")
	op = mustGetUser(t, pool, "op-exp")
	if !models.HasRole(op.Role, models.RoleOperator) {
		t.Fatalf("op must hold the operator role after redeeming the school voucher")
	}
	var opRedemptionID int
	var opRemainingBefore int64
	if err := pool.QueryRow(ctx,
		`SELECT id, remaining_seconds FROM voucher_redemptions WHERE user_id=$1 AND is_active`,
		op.ID).Scan(&opRedemptionID, &opRemainingBefore); err != nil {
		t.Fatalf("load op redemption: %v", err)
	}

	// The sub redeems its own guru voucher → an ACTIVE redemption whose clock
	// must be realigned when the operator's expiry changes (the cascade
	// touches every instansi account, own package or not).
	if status, resp := tc.createUser(t, "guru1"); status != http.StatusOK || !resp.Success {
		t.Fatalf("create guru1: status=%d resp=%+v", status, resp)
	}
	guru1 := mustGetUser(t, pool, "guru1")
	tc.login(t, guru1.ID)
	tc.redeem(t, "IT-GURU-SUB")
	var subRedemptionID int
	var subRemainingBefore int64
	if err := pool.QueryRow(ctx,
		`SELECT id, remaining_seconds FROM voucher_redemptions WHERE user_id=$1 AND is_active`,
		guru1.ID).Scan(&subRedemptionID, &subRemainingBefore); err != nil {
		t.Fatalf("load guru1 redemption: %v", err)
	}
	if subRemainingBefore < 20*86400 {
		t.Fatalf("guru1 redemption remaining=%d, want ~30 days before the edit", subRemainingBefore)
	}

	// A superadmin rewrites the operator's expiry to ~7 days out (Go clock,
	// truncated to seconds for a clean round-trip through the handler's
	// "2006-01-02 15:04:05" parse).
	newExpiry := time.Now().UTC().Add(7 * 24 * time.Hour).Truncate(time.Second)
	root, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "rootadmin-exp", Name: "Root Exp",
		PasswordHash: "pass-root-exp", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}
	tc.login(t, root.ID)
	status, resp := postJSON(t, tc.client, tc.srv, "/api/users/"+strconv.Itoa(op.ID)+"/edit",
		map[string]interface{}{"expires_at": newExpiry.Format("2006-01-02 15:04:05")})
	if status != http.StatusOK || !resp.Success {
		t.Fatalf("edit operator expiry: status=%d resp=%+v", status, resp)
	}

	// The instansi accounts' expires_at follow the operator's new expiry
	// (cascade) and their active package clocks were realigned to it: the
	// ~30 days remaining are rewritten to ~7 days — NOT preserved.
	// remaining_seconds is computed as EPOCH(expires_at - now()) inside the
	// DB: expires_at was written from the Go clock and now() is the DB clock,
	// so any host/container clock misalignment shifts the stored value by
	// that amount — the ±10 min tolerance below absorbs realistic skew.
	wantRemaining := int64(7 * 24 * time.Hour / time.Second)
	const tol int64 = 600 // ±10 min: absorbs clock alignment + the sync-to-read gap
	guru1 = mustGetUser(t, pool, "guru1")
	if guru1.ExpiresAt == nil || !approxEqual(*guru1.ExpiresAt, newExpiry, time.Minute) {
		t.Errorf("guru1 expires_at=%v, want ≈ %v (cascade)", guru1.ExpiresAt, newExpiry)
	}
	var subRemainingAfter int64
	if err := pool.QueryRow(ctx,
		`SELECT remaining_seconds FROM voucher_redemptions WHERE id=$1`,
		subRedemptionID).Scan(&subRemainingAfter); err != nil {
		t.Fatalf("load guru1 redemption after edit: %v", err)
	}
	if subRemainingAfter < wantRemaining-tol || subRemainingAfter > wantRemaining+tol {
		t.Errorf("guru1 redemption remaining after edit=%d, want ≈ %d (realigned from %d)",
			subRemainingAfter, wantRemaining, subRemainingBefore)
	}

	// The operator's own active package clock is realigned the same way
	// (SyncActiveRedemptionToExpiry, the single-user path).
	op = mustGetUser(t, pool, "op-exp")
	if op.ExpiresAt == nil || !approxEqual(*op.ExpiresAt, newExpiry, time.Minute) {
		t.Errorf("op expires_at=%v, want ≈ %v", op.ExpiresAt, newExpiry)
	}
	var opRemainingAfter int64
	if err := pool.QueryRow(ctx,
		`SELECT remaining_seconds FROM voucher_redemptions WHERE id=$1`,
		opRedemptionID).Scan(&opRemainingAfter); err != nil {
		t.Fatalf("load op redemption after edit: %v", err)
	}
	if opRemainingAfter < wantRemaining-tol || opRemainingAfter > wantRemaining+tol {
		t.Errorf("op redemption remaining after edit=%d, want ≈ %d (realigned from %d)",
			opRemainingAfter, wantRemaining, opRemainingBefore)
	}
}
