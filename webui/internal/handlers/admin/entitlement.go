package admin

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/models"
)

// packageEntitlement returns (exams, pdfBytes, concurrent, storageBytes,
// maxUsers, role) for a fixed package. `concurrent` is the max_concurrent_exams
// quota: the number of exams that may RUN simultaneously (mirrors the
// "Ujian Aktif" values advertised on the pricing page, so concurrent <= total
// exams). `maxUsers` is the sub-account quota: how many accounts (besides the
// operator) may exist in one school instansi; 0 = unlimited. These defaults
// mirror the package_settings seed in schema.sql (SuperAdmin-editable).
func packageEntitlement(pkg string) (exams, pdf, concurrent, storage, maxUsers int64, role string) {
	switch pkg {
	case "guru":
		return 1, 10 * 1024 * 1024, 1, 100 * 1024 * 1024, 0, ""
	case "individu":
		return 2, 30 * 1024 * 1024, 2, 300 * 1024 * 1024, 0, ""
	case "sekolah_kecil":
		return 3, 50 * 1024 * 1024, 3, 500 * 1024 * 1024, 10, models.SerializeRoles([]string{models.RoleOperator})
	case "sekolah_menengah":
		return 5, 200 * 1024 * 1024, 5, 2000 * 1024 * 1024, 25, models.SerializeRoles([]string{models.RoleOperator})
	case "sekolah_besar":
		return 10, 500 * 1024 * 1024, 10, 5000 * 1024 * 1024, 50, models.SerializeRoles([]string{models.RoleOperator})
	case "sekolah_unggulan":
		return 99999, 99999 * 1024 * 1024, 99999, 999999 * 1024 * 1024, 0, models.SerializeRoles([]string{models.RoleOperator})
	default:
		return 1, 1 * 1024 * 1024, 1, 50 * 1024 * 1024, 0, ""
	}
}

// schoolPoolCovers reports whether an operator-created sub-account currently
// draws its quotas from the school pool (its instansi has an active school
// package). When true the pool is the account's ONLY quota gate: its
// per-account columns — the forced free defaults, see subAccountFree* in
// users.go — are a display/fallback layer and must not cap exam usage below
// the school package (a sub-account in a 50MB-PDF school uploads up to 50MB,
// not the 1MB free default its row carries). Without a pool (shared
// "personal" bucket, legacy school without an active redemption) the
// per-account columns fall back into effect, which is exactly the state they
// are meant to cover.
func schoolPoolCovers(ctx context.Context, pool *pgxpool.Pool, user models.AdminUser) bool {
	if !user.OperatorCreated {
		return false
	}
	_, _, _, _, _, ok := schoolPoolQuota(ctx, pool, user.ID)
	return ok
}

// lockSchoolClaim takes the transaction-scoped school-claim advisory lock for
// the acting user's school, when that school is real (not empty / not the
// shared "personal" bucket). Every operator-granting path
// (redeem/activate/create/edit/claim) acquires this same lock keyed on the
// destination school, so the one-operator-per-school guard runs serially per
// school and can never race against a concurrent grant on the pre-commit
// snapshot. The user's instansi is read WITHOUT a lock — callers must re-read
// it authoritatively (FOR UPDATE) before the guard itself runs. The lock is
// transaction-scoped (released at commit/rollback, never leaked). Returns nil
// when there is no real school to lock (nothing to serialize).
func lockSchoolClaim(ctx context.Context, tx pgx.Tx, userID int) error {
	var inst string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(instansi, '') FROM admin_users WHERE id = $1`, userID).Scan(&inst); err != nil {
		return err
	}
	inst = strings.TrimSpace(inst)
	if inst == "" || strings.EqualFold(inst, "personal") {
		return nil
	}
	if _, err := tx.Exec(ctx,
		`SELECT pg_advisory_xact_lock(hashtext('school-claim:' || lower($1))::bigint)`, inst); err != nil {
		return err
	}
	return nil
}

// schoolAlreadyHasOperator reports whether a real school instansi already has
// ANOTHER real operator: an account NOT created by an operator (operator_created
// is the sub-account marker) holding the operator role. The ONE-OPERATOR-PER-
// SCHOOL policy rejects every path that would add a second operator to a
// school — redeeming/activating a school package, a SuperAdmin create/edit
// granting the role, and a personal-bucket operator claiming a school that
// already has one. The shared "personal" bucket is never a school: several
// personal-bucket operators may each hold their own school package until they
// claim a school. Legacy operator-created accounts holding their own operator
// role (pre-policy claims) are NOT the school's operator — they spare
// themselves from the suspend cascade but never block a new operator. The
// acting user (excludeID) is excluded, so the school's own operator renewing
// a school package is never blocked.
func schoolAlreadyHasOperator(ctx context.Context, q quotaQuerier, instansi string, excludeID int) (bool, error) {
	instansi = strings.TrimSpace(instansi)
	if instansi == "" || strings.EqualFold(instansi, "personal") {
		return false, nil
	}
	// System buckets ("owner" hosts the bootstrap superadmin) are never a
	// school: no operator-guard is evaluated against them.
	if isReservedInstansiName(instansi) {
		return false, nil
	}
	var has bool
	err := q.QueryRow(ctx, `
		SELECT EXISTS (
		    SELECT 1 FROM admin_users
		    WHERE LOWER(instansi) = LOWER($1) AND id <> $2
		      AND NOT operator_created
		      -- Exact operator-role match: JSON-quoted token or legacy bare
		      -- value — a plain substring ILIKE misses legacy non-JSON rows.
		      AND (role = 'operator' OR role ILIKE '%"operator"%')
		)`, instansi, excludeID).Scan(&has)
	if err != nil {
		return false, err
	}
	return has, nil
}

// schoolPoolQuota returns the shared "school pool" quota for the instansi the
// given user belongs to (see schoolPoolQuotaForInstansi). ok=false when the
// user has no instansi, sits in the shared "personal" bucket, or no operator
// in the instansi holds an active redemption.
func schoolPoolQuota(ctx context.Context, q quotaQuerier, userID int) (maxExams, maxPDF, maxConcurrent, maxStorage int64, scope models.InstansiScope, ok bool) {
	var inst string
	var id *int
	if err := q.QueryRow(ctx, `SELECT COALESCE(instansi, ''), instansi_id FROM admin_users WHERE id = $1`, userID).Scan(&inst, &id); err != nil {
		return 0, 0, 0, 0, models.InstansiScope{}, false
	}
	inst = strings.TrimSpace(inst)
	if inst == "" {
		return 0, 0, 0, 0, models.InstansiScope{}, false
	}
	scope = models.InstansiScope{ID: id, Name: inst}
	maxExams, maxPDF, maxConcurrent, maxStorage, ok = schoolPoolQuotaForInstansi(ctx, q, scope)
	return maxExams, maxPDF, maxConcurrent, maxStorage, scope, ok
}

// schoolPoolQuotaForInstansi returns the shared "school pool" quota for an
// instansi, sourced from the ACTIVE redemption(s) of the instansi's
// operator(s) — the same snapshot applyRedemptionEntitlement wrote to the
// operator's account at redeem time. In a school instansi EVERY account draws
// its exam/storage/PDF/concurrent quota from this single pool, the operator
// included: the school's total usage can never exceed the package, so five
// sub-accounts can no longer each spend a full package
// (5 × 3 exams ≫ a 3-exam school package).
//
// ok=false when no school pool applies: the instansi is empty or the shared
// "personal" bucket, or no operator in the instansi holds an active redemption
// (a legacy school without a package keeps the per-account quotas and the
// operator's historical bypass). Since the ONE-OPERATOR-PER-SCHOOL policy
// (schoolAlreadyHasOperator) a school has at most one operator; the MAX() over
// the theoretical multiple is a defensive fallback for legacy pre-policy
// states and stays never-surprising (the larger package wins). max_concurrent
// follows the same 0 → max_exams → 1 defaulting as
// applyRedemptionEntitlement so the two can never disagree.
func schoolPoolQuotaForInstansi(ctx context.Context, q quotaQuerier, scope models.InstansiScope) (maxExams, maxPDF, maxConcurrent, maxStorage int64, ok bool) {
	if scope.IsBucket() {
		return 0, 0, 0, 0, false
	}
	var n int
	pfrag, pargs := models.InstansiMatchSQL("u.", 1, scope)
	err := q.QueryRow(ctx, `
		SELECT COUNT(*),
		       COALESCE(MAX(vr.max_exams), 0),
		       COALESCE(MAX(vr.max_pdf_size), 0),
		       COALESCE(MAX(vr.max_concurrent_exams), 0),
		       COALESCE(MAX(vr.max_storage_size), 0)
		FROM voucher_redemptions vr
		JOIN admin_users u ON u.id = vr.user_id
		WHERE vr.is_active AND `+pfrag+
		  ` AND (u.role = 'operator' OR u.role ILIKE '%"operator"%')`, pargs...).
		Scan(&n, &maxExams, &maxPDF, &maxConcurrent, &maxStorage)
	if err != nil {
		log.Printf("load school pool quota failed: %v; treating as no school pool", err)
		return 0, 0, 0, 0, false
	}
	if n == 0 {
		return 0, 0, 0, 0, false
	}
	if maxConcurrent <= 0 {
		maxConcurrent = maxExams
	}
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}
	return maxExams, maxPDF, maxConcurrent, maxStorage, true
}

// familyExamBudget resolves the shared family budget for an
// operator-created sub-account that sits OUTSIDE any active school pool (the
// shared "personal" bucket, a legacy school without a redemption, or a
// label-drifted row): the creator-operator's quota is the reference, counted
// family-wide (creator + direct sub-accounts).
//
// Budget source mirrors loadOperatorAccountQuota's personal-bucket precedent:
// the creator's ACTIVE school pool when the creator's own scope runs one,
// else the creator's own per-account columns (the redeemed snapshot, or the
// free defaults) — so the family's total usage can never exceed what the
// operator's own account could spend alone.
//
// ok=false for non-sub-accounts, unattributed rows (created_by NULL or the
// creator row gone), and self-created cycles: those keep the existing
// per-account gates. Single level only (sub → creator), never recursive.
func familyExamBudget(ctx context.Context, q quotaQuerier, userID int) (maxExams, maxPDF, maxConcurrent, maxStorage int64, familyRoot int, ok bool) {
	var created bool
	var createdBy *int
	if err := q.QueryRow(ctx,
		`SELECT operator_created, created_by FROM admin_users WHERE id = $1`, userID).
		Scan(&created, &createdBy); err != nil {
		return 0, 0, 0, 0, 0, false
	}
	if !created || createdBy == nil || *createdBy <= 0 || *createdBy == userID {
		return 0, 0, 0, 0, 0, false
	}
	root := *createdBy
	// The creator's own active school pool (when the creator's scope runs
	// one) is the family budget — a drifted sub rejoins its school's pool
	// numbers while usage stays family-scoped.
	if me, mp, mc, ms, _, pok := schoolPoolQuota(ctx, q, root); pok {
		return me, mp, mc, ms, root, true
	}
	// Else the creator's own per-account columns are the reference.
	var cExams *int
	var cPDF *int
	var cConc *int
	var cStor *int64
	if err := q.QueryRow(ctx,
		`SELECT max_exams, max_pdf_size, max_concurrent_exams, max_storage_size FROM admin_users WHERE id = $1`, root).
		Scan(&cExams, &cPDF, &cConc, &cStor); err != nil {
		return 0, 0, 0, 0, 0, false
	}
	if cExams != nil {
		maxExams = int64(*cExams)
	}
	if cPDF != nil {
		maxPDF = int64(*cPDF)
	}
	if cConc != nil {
		maxConcurrent = int64(*cConc)
	}
	if cStor != nil {
		maxStorage = *cStor
	}
	// Same 0 → max_exams → 1 defaulting as schoolPoolQuotaForInstansi so the
	// family concurrent cap can never disagree with the pool semantics.
	if maxConcurrent <= 0 {
		maxConcurrent = maxExams
	}
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}
	return maxExams, maxPDF, maxConcurrent, maxStorage, root, true
}

// quotaGate is the effective exam-quota budget + usage scope for one account:
// either the shared school pool (tenant scope) or — for operator-created
// sub-accounts outside any pool — the creator-operator family budget (see
// familyExamBudget). All exam quota gates (create, storage, PDF, concurrent)
// resolve through examQuotaGate so sub-account usage always spends the same
// budget the operator's own uploads spend.
type quotaGate struct {
	maxExams, maxPDF, maxConcurrent, maxStorage int64
	active                                     bool
	pooled                                     bool // budget from an active school pool (vs family fallback)
	familyRoot                                 int  // >0: family mode, count across the creator family
	scope                                      models.InstansiScope
}

// examQuotaGate resolves the quota gate for userID: the own-scope school
// pool when active, else the creator-family budget for pool-less
// sub-accounts. Operators and pool-less non-sub-accounts get an inactive
// gate and keep their existing per-account gates.
func examQuotaGate(ctx context.Context, q quotaQuerier, userID int) quotaGate {
	if me, mp, mc, ms, scope, ok := schoolPoolQuota(ctx, q, userID); ok {
		return quotaGate{maxExams: me, maxPDF: mp, maxConcurrent: mc, maxStorage: ms,
			active: true, pooled: true, scope: scope}
	}
	if me, mp, mc, ms, root, ok := familyExamBudget(ctx, q, userID); ok {
		return quotaGate{maxExams: me, maxPDF: mp, maxConcurrent: mc, maxStorage: ms,
			active: true, familyRoot: root}
	}
	return quotaGate{}
}

// lockRows locks every account row the gate counts, so concurrent uploads /
// starts / toggles sharing one budget serialize on the same rows.
func (g quotaGate) lockRows(ctx context.Context, tx pgx.Tx) error {
	if g.familyRoot > 0 {
		_, err := tx.Exec(ctx,
			`SELECT id FROM admin_users WHERE id = $1 OR created_by = $1 FOR UPDATE`, g.familyRoot)
		return err
	}
	frag, args := models.InstansiMatchSQL("", 1, g.scope)
	_, err := tx.Exec(ctx, `SELECT id FROM admin_users WHERE `+frag+` FOR UPDATE`, args...)
	return err
}

// countExams returns the gate's usage: exams created by ANY account in the
// tenant (pool mode) or in the creator family (family mode).
func (g quotaGate) countExams(ctx context.Context, q quotaQuerier) (int64, error) {
	if g.familyRoot > 0 {
		return models.CountExamsByFamily(ctx, q, g.familyRoot)
	}
	return models.CountExamsByInstansi(ctx, q, g.scope)
}

// sumStorage returns the gate's storage usage (PDF bytes).
func (g quotaGate) sumStorage(ctx context.Context, q quotaQuerier) (int64, error) {
	if g.familyRoot > 0 {
		return models.SumStorageByFamily(ctx, q, g.familyRoot)
	}
	return models.SumStorageByInstansi(ctx, q, g.scope)
}

// countRunning returns the gate's concurrent usage (running exams).
func (g quotaGate) countRunning(ctx context.Context, q quotaQuerier, excludeID int) (int, error) {
	if g.familyRoot > 0 {
		return models.CountRunningExamsByFamily(ctx, q, g.familyRoot, excludeID)
	}
	return models.CountRunningExamsByInstansi(ctx, q, g.scope, excludeID)
}

// enforceBulkFamilyConcurrent gates bulk activation for accounts sharing
// one budget outside any school pool: pool-less sub-accounts (each creator
// family's running-after count must fit the family budget, see
// familyExamBudget) and pool-less operators (the operator's own running-after
// count is family-wide, symmetric with the single-exam paths). Pool-covered
// exams are handled by the tenant pool check; other per-owner columns by the
// caller's per-owner loop. The caller's instansi name-lock already covers the
// family rows (same or shared-bucket labels), mirroring the existing lock
// granularity. Fail-open on query errors, like the surrounding bulk guards
// (which only enforce when cErr == nil).
func enforceBulkFamilyConcurrent(ctx context.Context, tx pgx.Tx, examIDs []int) error {
	if len(examIDs) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `
		SELECT e.created_by, e.status, (e.exam_started_at IS NOT NULL),
		       COALESCE(u.role, ''), COALESCE(u.max_concurrent_exams, 0)
		FROM exams e JOIN admin_users u ON u.id = e.created_by
		WHERE e.id = ANY($1)`, examIDs)
	if err != nil {
		return nil
	}
	type bulkRow struct {
		createdBy int
		status    string
		started   bool
		role      string
		maxConc   int
	}
	var list []bulkRow
	for rows.Next() {
		var r bulkRow
		if err := rows.Scan(&r.createdBy, &r.status, &r.started, &r.role, &r.maxConc); err != nil {
			continue
		}
		list = append(list, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil
	}
	gates := map[int]quotaGate{}
	opMaxConc := map[int]int{}
	activating := map[int]int{}
	member := map[int]int{}
	roots := map[int]bool{}
	for _, r := range list {
		createdBy := r.createdBy
		status := r.status
		role := r.role
		started := r.started
		g, ok := gates[createdBy]
		if !ok {
			g = examQuotaGate(ctx, tx, createdBy)
			gates[createdBy] = g
		}
		root := 0
		if g.familyRoot > 0 {
			// Pool-less sub-account: the creator family's budget.
			root = g.familyRoot
			if _, seen := member[root]; !seen {
				member[root] = createdBy
			}
		} else if !g.pooled && models.HasRole(role, models.RoleOperator) {
			// Pool-less operator: its own budget, counted family-wide
			// (symmetric with the single-exam paths). A pooled operator
			// stays under the tenant pool check only.
			root = createdBy
			opMaxConc[root] = r.maxConc
		}
		if root == 0 {
			continue
		}
		roots[root] = true
		if started && status != "active" {
			activating[root]++
		}
	}
	for root := range roots {
		running, err := models.CountRunningExamsByFamily(ctx, tx, root, 0)
		if err != nil {
			continue
		}
		maxConc := 0
		if sub, ok := member[root]; ok {
			_, _, mc, _, _, ok := familyExamBudget(ctx, tx, sub)
			if !ok {
				continue
			}
			maxConc = int(mc)
		} else {
			maxConc = opMaxConc[root]
			if maxConc <= 0 {
				continue
			}
		}
		// `after > max` (not >=) matches the single-exam semantics:
		// reaching the limit is allowed, only exceeding it is rejected.
		if running+activating[root] > maxConc {
			return fmt.Errorf("Batas ujian serentak tercapai. Maksimal %d ujian dapat berjalan bersamaan (kuota operator).", maxConc)
		}
	}
	return nil
}

// schoolPackageLabel resolves the display label of the active school
// package running an instansi: the mapped package name ("Paket …") with a
// generic "Paket Sekolah" fallback when no snapshot row names it.
func schoolPackageLabel(ctx context.Context, pool *pgxpool.Pool, instansi string) string {
	var schoolPkg string
	if err := pool.QueryRow(ctx, `
			SELECT COALESCE(vr.package, '')
			FROM voucher_redemptions vr
			JOIN admin_users u ON u.id = vr.user_id
			WHERE vr.is_active AND LOWER(u.instansi) = LOWER($1)
			  AND (u.role = 'operator' OR u.role ILIKE '%"operator"%')
			ORDER BY vr.redeemed_at DESC, vr.id DESC
			LIMIT 1`, instansi).Scan(&schoolPkg); err != nil || schoolPkg == "" {
		return "Paket Sekolah"
	}
	return packageDisplayName(schoolPkg)
}

// effectiveSubQuota returns the quota numbers + package label a sub-account's
// cards must show instead of its forced free-default columns: the school
// pool when covered, else the creator-family budget (see familyExamBudget).
// ok=false → the caller keeps the per-account columns. Shared by the
// billing overlay and the dashboard info card so display and enforcement
// (examQuotaGate) can never disagree.
func effectiveSubQuota(ctx context.Context, pool *pgxpool.Pool, user models.AdminUser) (label string, maxExams, maxPDF, maxConc, maxStorage int64, ok bool) {
	if !user.OperatorCreated {
		return "", 0, 0, 0, 0, false
	}
	if me, mp, mc, ms, _, pok := schoolPoolQuota(ctx, pool, user.ID); pok {
		return schoolPackageLabel(ctx, pool, user.Instansi), me, mp, mc, ms, true
	}
	if g := examQuotaGate(ctx, pool, user.ID); g.active && g.familyRoot > 0 {
		return "Paket Operator", g.maxExams, g.maxPDF, g.maxConcurrent, g.maxStorage, true
	}
	return "", 0, 0, 0, 0, false
}

// durationDays maps a voucher duration type ("bulanan", "semester", "tahunan",
// or a positive integer as days) to the number of days the entitlement lasts.
func durationDays(durationType string) int {
	switch durationType {
	case "bulanan":
		return 30
	case "semester":
		return 180
	case "tahunan":
		return 365
	default:
		if d, err := strconv.Atoi(durationType); err == nil && d > 0 {
			return d
		}
		return 30
	}
}

// containsRole reports whether roles contains target.
func containsRole(roles []string, target string) bool {
	for _, r := range roles {
		if r == target {
			return true
		}
	}
	return false
}

// applyRedemptionEntitlement applies a voucher redemption's snapshot to a user:
// overwrites the quota columns with the snapshot values, sets the account
// expiry to the redemption's current running period (activated_at +
// remaining_seconds), and makes the role follow the ACTIVE package. The caller
// must have set ActivatedAt (the start of the currently-running period) and
// RemainingSeconds before calling.
//
// Roles are tracked in two columns:
//   - base_role: roles the user holds independently of packages (identity,
//     admin grants, delegated pengawas);
//   - package_role: roles granted by the currently-ACTIVE package.
//
// On activation the role becomes base_role ∪ new package roles, so
// package-granted roles are transient (an operator from a sekolah package
// disappears when a guru voucher is activated and returns when a sekolah
// package is active again) while admin-granted roles survive a switch even
// when they coincide with what a package grants. base_role is initialized
// lazily from the current roles minus the current package's grant (fresh
// accounts and pre-migration rows). A SuperAdmin's role is never changed.
func applyRedemptionEntitlement(ctx context.Context, tx pgx.Tx, userID int, r *models.VoucherRedemption) error {
	if r.ActivatedAt == nil {
		return fmt.Errorf("redemption has no running period (activated_at is nil)")
	}
	if r.RemainingSeconds <= 0 {
		return fmt.Errorf("redemption has no remaining lifetime")
	}
	expiry := r.ActivatedAt.Add(time.Duration(r.RemainingSeconds) * time.Second)

	var currentRoleJSON, currentPkgRoleJSON, currentBaseRoleJSON string
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(role, ''), COALESCE(package_role, ''), COALESCE(base_role, '') FROM admin_users WHERE id = $1`,
		userID).Scan(&currentRoleJSON, &currentPkgRoleJSON, &currentBaseRoleJSON); err != nil {
		return err
	}

	// Roles granted by the package being activated ('' = grants no role).
	pkgRole := strings.TrimSpace(r.Role)
	roleJSON, nextPkgRole, nextBaseRole := nextRolesState(currentRoleJSON, currentPkgRoleJSON, currentBaseRoleJSON, pkgRole)

	pkgLabel := strings.TrimSpace(r.Package)
	if pkgLabel == "" {
		pkgLabel = "custom"
	}

	concurrent := r.MaxConcurrentExams
	if concurrent <= 0 {
		concurrent = r.MaxExams
	}
	if concurrent <= 0 {
		concurrent = 1
	}

	// Never touch the account's status here: expired accounts keep status
	// 'active' (expiry is time-based), and a suspended account must STAY
	// suspended — writing status = 'active' would let a suspended user
	// self-reactivate by redeeming/activating a voucher (the caller already
	// rejects suspended accounts, this is the last line of defense).
	_, err := tx.Exec(ctx, `UPDATE admin_users SET
		package = $1, max_exams = $2, max_pdf_size = $3,
		max_concurrent_exams = $4, max_storage_size = $5,
		expires_at = $6, role = $7, package_role = $8, base_role = $9
		WHERE id = $10`,
		pkgLabel, r.MaxExams, r.MaxPDFSize, concurrent, r.MaxStorageSize, expiry,
		roleJSON, nextPkgRole, nextBaseRole, userID)
	if err != nil {
		return err
	}

	// Keep the school's sub-accounts consistent with the package-granted
	// operator role (see syncInstansiWithOperatorRole).
	return syncInstansiWithOperatorRole(ctx, tx, userID, currentRoleJSON, roleJSON, expiry)
}

// syncInstansiWithOperatorRole keeps the accounts of a school instansi in sync
// with the package-granted operator role after a package activation:
//
//   - role HAS operator: restore accounts that were cascade-suspended when the
//     operator left the school package (freezing their clocks for the
//     suspension period), then align the expiry of sub-accounts that do not
//     run their own active package to the operator's new expiry — a school
//     operator's accounts follow the operator's expiry;
//   - role LOST operator: suspend the instansi's active non-operator accounts
//     (suspended_by_cascade), so accounts created under a school package stop
//     being usable once the operator leaves the school package. Their exams
//     that are active but have never been started (exam_started_at IS NULL)
//     are tombstoned to inactive too, so the school's unpublished exams go
//     dormant the moment the operator role is lost — exams already running
//     are left untouched so students working on them can finish.
//
// Pure guru → guru switches (no operator involved at all) are no-ops. The
// operator's own row, SuperAdmin, and manually suspended accounts are never
// touched. Must be called inside the redemption transaction, after the user's
// role columns have been updated.
func syncInstansiWithOperatorRole(ctx context.Context, tx pgx.Tx, userID int, prevRoleJSON, roleJSON string, expiry time.Time) error {
	if containsRole(models.ParseRoles(roleJSON), models.RoleSuperAdmin) {
		return nil // SuperAdmin role is never touched anywhere.
	}

	var inst string
	var id *int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(instansi, ''), instansi_id FROM admin_users WHERE id = $1`, userID).Scan(&inst, &id); err != nil {
		return err
	}
	instansi := strings.TrimSpace(inst)
	if instansi == "" || strings.EqualFold(instansi, "personal") {
		return nil // No school, no sub-accounts.
	}
	scope := models.InstansiScope{ID: id, Name: instansi}

	switch operatorRoleTransition(prevRoleJSON, roleJSON) {
	case "none":
		return nil // e.g. guru → guru: nothing to reconcile.
	case "suspend":
		// A school can legitimately have MORE than one operator (several gurus
		// can each redeem a school voucher, SuperAdmin can create co-operators)
		// and the school pool deliberately MAXes their active redemptions
		// (schoolPoolQuotaForInstansi). So the operator role being gone from
		// the ACTING user does not mean the school lost its coverage: when
		// ANOTHER REAL operator of the instansi still holds an active
		// redemption, the school package is still live and its sub-accounts
		// must keep working — the suspend/tombstone cascade is skipped
		// entirely. Only when no other operator covers the school does the
		// school genuinely lose its package, and the cascade fires as
		// documented. Coverage counts REAL operators only (NOT operator_created
		// — a sub-account holding its own operator role is an exception that
		// spares ITSELF from the cascade, but it does not run the school:
		// the documented sub-account semantics keep the plain subs suspended
		// when the actual operator leaves).
		var covered bool
		cfrag, cargs := models.InstansiMatchSQL("u.", 1, scope)
		cargs = append(cargs, userID)
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
			    SELECT 1 FROM admin_users u
			    JOIN voucher_redemptions vr ON vr.user_id = u.id
			    WHERE `+cfrag+` AND u.id <> `+fmt.Sprintf("$%d", len(cargs))+`
			      AND (u.role = 'operator' OR u.role ILIKE '%"operator"%') AND vr.is_active
			      AND NOT u.operator_created
			)`, cargs...).Scan(&covered); err != nil {
			return err
		}
		if covered {
			return nil // another operator still runs the school — nothing to reconcile
		}
		// The operator role is gone and no other operator covers the school:
		// suspend every active non-operator account in the instansi so
		// accounts created under the school package stop working.
		// suspended_by_cascade lets them come back (with a clock freeze) when
		// an operator returns to a school package.
		sfrag, sargs := models.InstansiMatchSQL("", 1, scope)
		sargs = append(sargs, userID)
		if _, err := tx.Exec(ctx, `
			UPDATE admin_users
			SET status = 'suspended', suspended_by_cascade = TRUE, suspended_at = now()
			WHERE `+sfrag+` AND status = 'active' AND id <> `+fmt.Sprintf("$%d", len(sargs))+`
			  AND NOT (role = 'operator' OR role ILIKE '%"operator"%')`, sargs...); err != nil {
			return err
		}
		// Tombstone the school's unpublished exams (policy B): the accounts
		// cut off from the school package (the operator who just lost the
		// role and every cascade-suspended sub) have their active-but-unstarted
		// exams set inactive. Accounts that still hold the operator role (their
		// own school voucher) stay valid operators, so their exams are spared.
		return tombstoneUnstartedInstansiExams(ctx, tx, scope, true)
	default: // restore
		// Operator role is present: restore accounts that were cascade-suspended
		// when the operator last left the school package (clock freeze applies)
		// and realign their active package clocks with the frozen expiry.
		if err := models.RestoreCascadeSuspendedInstansi(ctx, tx, scope, userID); err != nil {
			return err
		}
		// Sub-accounts that do not run their own active package follow the
		// operator's expiry. GREATEST only ever EXTENDS an existing expiry
		// (never shortens a sub-account when the operator switches to a
		// shorter school package); when the operator truly leaves the school
		// package the suspend branch above takes over.
		rfrag, rargs := models.InstansiMatchSQL("u.", 1, scope)
		expIdx := len(rargs) + 1
		uidIdx := len(rargs) + 2
		rargs = append(rargs, expiry, userID)
		_, err := tx.Exec(ctx, fmt.Sprintf(`
			UPDATE admin_users u
			SET expires_at = GREATEST(COALESCE(u.expires_at, $%d::timestamptz), $%[1]d::timestamptz)
			WHERE `+rfrag+` AND u.id <> `+fmt.Sprintf("$%d", uidIdx)+`
			  AND NOT (u.role = 'operator' OR u.role ILIKE '%%"operator"%%')
			  AND NOT EXISTS (
			      SELECT 1 FROM voucher_redemptions vr
			      WHERE vr.user_id = u.id AND vr.is_active
			  )`, expIdx), rargs...)
		return err
	}
}

// tombstoneUnstartedInstansiExams (policy B) sets every exam in the instansi
// that is active but has never been started (exam_started_at IS NULL) to
// inactive — the school's unpublished exams go dormant when the school's
// operator is cut off (voucher switch or manual suspension). Exams already
// running are deliberately left untouched so students working on them can
// finish, and the tombstone is never auto-reversed: the owner re-activates
// manually, so an admin's explicit inactivation is never clobbered.
//
// spareOperatorRoleCreators controls which exams are covered. When true
// (voucher switch), accounts that still hold the operator role (their own
// school voucher) remain valid operators, so their exams are spared; when
// false (a manual operator suspension freezes the whole school), every
// account in the instansi is covered.
func tombstoneUnstartedInstansiExams(ctx context.Context, exec models.Executor, scope models.InstansiScope, spareOperatorRoleCreators bool) error {
	if scope.IsBucket() {
		return nil
	}
	creatorFilter := `TRUE`
	if spareOperatorRoleCreators {
		creatorFilter = `NOT (role = 'operator' OR role ILIKE '%"operator"%')`
	}
	// tombstoned_at marks the exam as auto-inactivated (policy B) so the admin
	// UI can tell it apart from a manual inactivation; the marker is cleared
	// whenever the exam is (re)activated.
	tfrag, targs := models.InstansiMatchSQL("", 1, scope)
	_, err := exec.Exec(ctx, fmt.Sprintf(`
		UPDATE exams e
		SET status = 'inactive', tombstoned_at = now()
		WHERE e.status = 'active' AND e.exam_started_at IS NULL
		  AND e.created_by IN (
		      SELECT id FROM admin_users
			      WHERE %s AND %s
		  )`, tfrag, creatorFilter), targs...)
	return err
}

// operatorRoleTransition decides how the school's sub-accounts must react to a
// package activation based on the operator role before and after:
//   - "suspend": the operator role was lost — sub-accounts are deactivated;
//   - "restore": the operator role is present (gained or retained) —
//     cascade-suspended sub-accounts are restored and their expiry realigned;
//   - "none": no operator role on either side — nothing to reconcile.
//
// Pure function (no DB access) so the school → guru → school transition can be
// unit-tested directly; the SQL wrapper is syncInstansiWithOperatorRole.
func operatorRoleTransition(prevRoleJSON, roleJSON string) string {
	prevHasOp := containsRole(models.ParseRoles(prevRoleJSON), models.RoleOperator)
	newHasOp := containsRole(models.ParseRoles(roleJSON), models.RoleOperator)
	if !prevHasOp && !newHasOp {
		return "none"
	}
	if newHasOp {
		return "restore"
	}
	return "suspend"
}

// nextRolesState computes the (role, package_role, base_role) columns for a
// package activation from the user's current columns and the package's
// snapshot role. It is a pure function (no DB access) so the "role follows
// the active package" semantics can be unit-tested directly; the SQL wrapper
// is applyRedemptionEntitlement.
//
// Roles are tracked in two columns:
//   - base_role: roles the user holds independently of packages (identity,
//     admin grants, delegated pengawas);
//   - package_role: roles granted by the currently-ACTIVE package.
//
// The resulting role is base_role ∪ new package roles, so package-granted
// roles are transient (an operator from a sekolah package disappears when a
// guru voucher is activated and returns when a sekolah package is active
// again) while admin-granted roles survive a switch even when they coincide
// with what a package grants. base_role is initialized lazily from the
// current roles minus the current package's grant (fresh accounts and
// pre-migration rows). A SuperAdmin's role is never changed.
func nextRolesState(currentRoleJSON, currentPkgRoleJSON, currentBaseRoleJSON, newPkgRole string) (roleJSON, nextPkgRole, nextBaseRole string) {
	newPkgRoles := parsePackageRoles(newPkgRole)
	currentRoles := models.ParseRoles(currentRoleJSON)

	nextPkgRole = newPkgRole
	nextBaseRole = currentBaseRoleJSON

	if containsRole(currentRoles, models.RoleSuperAdmin) {
		// SuperAdmin: role is never touched, and package roles are never
		// applied to (or tracked on) the account.
		return models.SerializeRoles(currentRoles), "", currentBaseRoleJSON
	}

	// Defense-in-depth: a package must never grant the superadmin role to a
	// user who is not already superadmin. Every creation path (custom voucher
	// role, package_settings) whitelists roles, but a direct DB edit could
	// inject "superadmin" — strip it here at the last line of defense and
	// never track it in package_role.
	sanitized := newPkgRoles[:0]
	for _, r := range newPkgRoles {
		if r != models.RoleSuperAdmin {
			sanitized = append(sanitized, r)
		}
	}
	if len(sanitized) != len(newPkgRoles) {
		newPkgRoles = sanitized
		nextPkgRole = models.SerializeRoles(sanitized)
		if len(sanitized) == 0 {
			nextPkgRole = ""
		}
	}

	// The base is the user's roles beyond packages. Once tracked it is
	// preserved across switches; for fresh/pre-migration accounts it is
	// derived lazily as the current roles minus the current package's grant
	// (so a legacy accumulated operator is not baked in when the active
	// package never granted it).
	base := parsePackageRoles(currentBaseRoleJSON)
	if currentBaseRoleJSON == "" {
		prevPkgRoles := parsePackageRoles(currentPkgRoleJSON)
		base = make([]string, 0, len(currentRoles))
		for _, rr := range currentRoles {
			if !containsRole(prevPkgRoles, rr) {
				base = append(base, rr)
			}
		}
	}

	// role = base ∪ new package roles.
	merged := make([]string, 0, len(base)+len(newPkgRoles))
	for _, rr := range base {
		if !containsRole(merged, rr) {
			merged = append(merged, rr)
		}
	}
	for _, rr := range newPkgRoles {
		if !containsRole(merged, rr) {
			merged = append(merged, rr)
		}
	}
	if len(merged) == 0 {
		merged = []string{models.RoleGuru} // always keep the base role
	}
	if len(base) == 0 {
		base = []string{models.RoleGuru}
	}
	return models.SerializeRoles(merged), nextPkgRole, models.SerializeRoles(base)
}

// parsePackageRoles parses a package-granted role string, treating an empty
// value as "no roles". Unlike models.ParseRoles, which defaults an empty
// string to [guru], a package only grants exactly what its snapshot stores.
// An empty JSON array ("[]") is also treated as no roles, so hand-edited
// rows never produce a literal "[]" role via ParseRoles' fallback.
func parsePackageRoles(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" || s == "[]" {
		return nil
	}
	return models.ParseRoles(s)
}

// pauseActiveRedemption freezes the user's currently-active redemption: its
// remaining_seconds is reduced by the time elapsed since activated_at, then the
// row is marked inactive. No-op when the user has no active package. This is
// the automatic "pause" — there is no user-facing pause/resume control.
func pauseActiveRedemption(ctx context.Context, tx pgx.Tx, userID int) error {
	_, err := tx.Exec(ctx, `
		UPDATE voucher_redemptions
		SET remaining_seconds = GREATEST(
				remaining_seconds -
				COALESCE(EXTRACT(EPOCH FROM (now() - activated_at))::bigint, 0),
				0),
			activated_at = NULL,
			is_active = false
		WHERE user_id = $1 AND is_active`, userID)
	return err
}
