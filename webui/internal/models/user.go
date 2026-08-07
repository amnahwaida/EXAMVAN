package models

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/pbkdf2"
	"golang.org/x/crypto/scrypt"
)

// Valid status values for admin_users.
const (
	UserStatusActive     = "active"
	UserStatusSuspended  = "suspended"
	UserStatusPendingOTP = "pending_otp"
)

// Valid role values within the JSON role array.
const (
	RoleGuru       = "guru"
	RolePengawas   = "pengawas"
	RoleOperator   = "operator"
	RoleSuperAdmin = "superadmin"
)

// usernameRe restricts usernames to lowercase letters, digits, dot, underscore
// and hyphen, 3-32 characters. This charset contains no HTML/JS metacharacters,
// preventing stored XSS when usernames are rendered by the admin UI (which
// builds HTML via innerHTML and is not protected by html/template escaping).
var usernameRe = regexp.MustCompile(`^[a-z0-9._-]{3,32}$`)

// IsValidUsername reports whether s is an acceptable username. Callers should
// TrimSpace/ToLower before calling.
func IsValidUsername(s string) bool {
	return usernameRe.MatchString(s)
}

type AdminUser struct {
	ID                 int        `json:"id"`
	Username           string     `json:"username"`
	Name               string     `json:"name"`
	PasswordHash       string     `json:"-"` // never serialized
	CreatedAt          time.Time  `json:"created_at"`
	Status             string     `json:"status"`
	Instansi           string     `json:"instansi"`
	InstansiID         *int       `json:"instansi_id,omitempty"`
	Role               string     `json:"role"`         // JSON array string e.g. '["guru"]', or 'superadmin'
	BaseRole           string     `json:"base_role"`    // roles held independently of packages (JSON array string)
	PackageRole        string     `json:"package_role"` // roles granted by the currently-ACTIVE package (JSON array string)
	MaxExams           int        `json:"max_exams"`
	MaxPDFSize         int        `json:"max_pdf_size"`
	MaxConcurrentExams int        `json:"max_concurrent_exams"`
	MaxStorageSize     int64      `json:"max_storage_size"`
	WhatsappNumber     string     `json:"whatsapp_number"`
	Email              string     `json:"email"`
	RegisteredIP       string     `json:"registered_ip"` // IP the account was registered from (anti mass-registration)
	ExpiresAt          *time.Time `json:"expires_at,omitempty"`
	Package            string     `json:"package"`
	OTPCode            *string    `json:"-"`
	OTPExpiry          *time.Time `json:"-"`
}

// Roles parses the Role JSON string and returns the list of roles.
// Handles the hardcoded 'superadmin' value and JSON arrays.
func (u *AdminUser) Roles() []string {
	return ParseRoles(u.Role)
}

// SetRoles serializes a roles list into the Role field.
func (u *AdminUser) SetRoles(roles []string) {
	u.Role = SerializeRoles(roles)
}

// HasRole checks if the user has a specific role.
func (u *AdminUser) HasRole(target string) bool {
	return HasRole(u.Role, target)
}

// IsSuperAdmin returns true if the user is the super admin.
func (u *AdminUser) IsSuperAdmin() bool {
	return u.Role == RoleSuperAdmin || u.HasRole(RoleSuperAdmin)
}

// IsOperator returns true if the user has the operator role.
func (u *AdminUser) IsOperator() bool {
	return u.HasRole(RoleOperator)
}

// IsGuru returns true if the user has the guru role.
func (u *AdminUser) IsGuru() bool {
	return u.HasRole(RoleGuru)
}

// IsPengawas returns true if the user has the pengawas role.
func (u *AdminUser) IsPengawas() bool {
	return u.HasRole(RolePengawas)
}

// IsActive returns true when the user status is "active".
func (u *AdminUser) IsActive() bool { return u.Status == UserStatusActive }

// IsExpired checks whether the account has passed its expiry date.
func (u *AdminUser) IsExpired() bool {
	if u.ExpiresAt == nil {
		return false
	}
	return time.Now().UTC().After(*u.ExpiresAt)
}

// ParseRoles parses a role string into a slice of role names.
// Handles the hardcoded 'superadmin' value and JSON array formats.
func ParseRoles(roleStr string) []string {
	if roleStr == "" {
		return []string{RoleGuru}
	}
	if roleStr == RoleSuperAdmin {
		return []string{RoleSuperAdmin}
	}
	var roles []string
	if err := json.Unmarshal([]byte(roleStr), &roles); err == nil {
		if len(roles) > 0 {
			return roles
		}
	}
	return []string{roleStr}
}

// HasRole checks if a role string includes the target role.
func HasRole(roleStr, target string) bool {
	for _, r := range ParseRoles(roleStr) {
		if r == target {
			return true
		}
	}
	return false
}

// SerializeRoles serializes a slice of roles to a JSON string.
func SerializeRoles(roles []string) string {
	b, _ := json.Marshal(roles)
	return string(b)
}

// DisplayRoles returns human-readable role labels from a role string.
func DisplayRoles(roleStr string) string {
	roleMap := map[string]string{
		RoleSuperAdmin: "Super Admin",
		RoleGuru:       "Guru",
		RolePengawas:   "Pengawas",
		RoleOperator:   "Operator",
	}
	roles := ParseRoles(roleStr)
	labels := make([]string, 0, len(roles))
	for _, r := range roles {
		if label, ok := roleMap[r]; ok {
			labels = append(labels, label)
		} else {
			labels = append(labels, r)
		}
	}
	if len(labels) == 0 {
		return "Guru"
	}
	result := labels[0]
	for i := 1; i < len(labels); i++ {
		result += ", " + labels[i]
	}
	return result
}

// Password helpers.

// HashPassword generates a bcrypt hash of the password.
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(bytes), nil
}

// CheckPassword compares a password against a hash.
// Supports bcrypt hashes (starts with "$2") and werkzeug-style hashes
// (starts with "scrypt:" or "pbkdf2:") from the Python EXAMVAN server.
func CheckPassword(password, hash string) bool {
	if hash == "" {
		return false
	}
	// bcrypt
	if strings.HasPrefix(hash, "$2") {
		err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
		return err == nil
	}
	// werkzeug scrypt/pbkdf2: Python uses scrypt:scrypt:...:salt:hash
	// or pbkdf2:sha256:iterations:salt:hash
	// For werkzeug hashes, we need to use the same algorithm.
	// The Python server stores hashes in werkzeug format, so we need
	// to handle the scrypt format specifically.
	if strings.HasPrefix(hash, "scrypt:") {
		return checkWerkzeugScrypt(password, hash)
	}
	if strings.HasPrefix(hash, "pbkdf2:") {
		return checkWerkzeugPbkdf2(password, hash)
	}
	// Fallback: try bcrypt
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// checkWerkzeugScrypt verifies a werkzeug scrypt hash.
// Format: scrypt:N:r:p$base64salt$hexhash
func checkWerkzeugScrypt(password, hash string) bool {
	// werkzeug scrypt hashes look like: scrypt:32768:8:1$salt$hash
	parts := strings.Split(hash, "$")
	if len(parts) < 3 {
		return false
	}

	// Parse N, r, p from the algorithm part
	algoPart := parts[0] // "scrypt:32768:8:1"
	algoParts := strings.Split(algoPart, ":")
	N := 32768
	r := 8
	p := 1
	if len(algoParts) >= 4 {
		fmt.Sscanf(algoParts[1], "%d", &N)
		fmt.Sscanf(algoParts[2], "%d", &r)
		fmt.Sscanf(algoParts[3], "%d", &p)
	}

	// Decode base64 salt (werkzeug may strip padding)
	saltB64 := parts[1]
	// Add padding if needed
	switch len(saltB64) % 4 {
	case 2:
		saltB64 += "=="
	case 3:
		saltB64 += "="
	}
	salt, err := base64.StdEncoding.DecodeString(saltB64)
	if err != nil {
		return false
	}

	expectedHashHex := parts[2]
	expectedHash, err := hex.DecodeString(expectedHashHex)
	if err != nil {
		return false
	}

	dk, err := scrypt.Key([]byte(password), salt, N, r, p, len(expectedHash))
	if err != nil {
		return false
	}

	return subtle.ConstantTimeCompare(dk, expectedHash) == 1
}

// EnsureAdminUser creates the admin user if it doesn't exist, using bcrypt
// for password hashing. This is called on startup to ensure the admin user
// is available.
func EnsureAdminUser(ctx context.Context, pool *pgxpool.Pool, adminUser, adminPass string) error {
	if adminUser == "" || adminPass == "" {
		return nil // Skip if not configured
	}

	// Check if user exists
	_, err := GetUserByUsername(ctx, pool, adminUser)
	if err == nil {
		return nil // User already exists
	}
	if err != pgx.ErrNoRows {
		return fmt.Errorf("failed to check admin user: %w", err)
	}

	// Create admin user with bcrypt password
	hash, err := HashPassword(adminPass)
	if err != nil {
		return fmt.Errorf("failed to hash admin password: %w", err)
	}

	roleJSON := SerializeRoles([]string{RoleSuperAdmin})
	_, err = pool.Exec(ctx,
		`INSERT INTO admin_users (username, password_hash, role, instansi, status) VALUES ($1, $2, $3, $4, $5)`,
		adminUser, hash, roleJSON, "owner", UserStatusActive)
	if err != nil {
		return fmt.Errorf("failed to create admin user: %w", err)
	}

	log.Printf("Admin user '%s' created successfully", adminUser)
	return nil
}

// MigrateAdminPassword checks if the admin user's password is in werkzeug
// format and re-hashes it using bcrypt. This is needed because werkzeug's
// scrypt/pbkdf2 hashes are not natively supported by Go.
func MigrateAdminPassword(ctx context.Context, pool *pgxpool.Pool, adminUser, adminPass string) error {
	user, err := GetUserByUsername(ctx, pool, adminUser)
	if err != nil {
		return err
	}

	// Check if hash is already bcrypt
	if strings.HasPrefix(user.PasswordHash, "$2") {
		return nil // Already bcrypt
	}

	// Check if the password actually matches the werkzeug hash
	if !CheckPassword(adminPass, user.PasswordHash) {
		return nil // Password doesn't match, don't migrate
	}

	// Re-hash with bcrypt
	newHash, err := HashPassword(adminPass)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	_, err = pool.Exec(ctx,
		`UPDATE admin_users SET password_hash = $1 WHERE id = $2`,
		newHash, user.ID)
	if err != nil {
		return fmt.Errorf("failed to update password hash: %w", err)
	}

	log.Printf("Admin password hash migrated from werkzeug to bcrypt for user '%s'", adminUser)
	return nil
}

// checkWerkzeugPbkdf2 verifies a werkzeug pbkdf2 hash.
// Format: pbkdf2:sha256:iterations:salt:hash
func checkWerkzeugPbkdf2(password, hash string) bool {
	parts := strings.Split(hash, ":")
	if len(parts) < 5 {
		return false
	}

	iterations := 600000 // default werkzeug iterations
	fmt.Sscanf(parts[2], "%d", &iterations)

	salt := parts[3]
	expectedHashHex := parts[4]

	expectedHash, err := hex.DecodeString(expectedHashHex)
	if err != nil {
		return false
	}

	dk := pbkdf2.Key([]byte(password), []byte(salt), iterations, 32, sha256.New)
	return subtle.ConstantTimeCompare(dk, expectedHash) == 1
}

// DefaultAdminUserColumns is the column list for admin_users SELECT queries.
const DefaultAdminUserColumns = `id, username, name, password_hash, created_at, status,
instansi, role, max_exams, max_pdf_size, max_concurrent_exams,
max_storage_size, whatsapp_number, email, expires_at, otp_code, otp_expiry, package,
base_role, package_role`

// scanAdminUser scans a row into an AdminUser struct.
func scanAdminUser(row pgx.Row) (AdminUser, error) {
	var u AdminUser
	err := row.Scan(
		&u.ID, &u.Username, &u.Name, &u.PasswordHash, &u.CreatedAt, &u.Status,
		&u.Instansi, &u.Role, &u.MaxExams, &u.MaxPDFSize, &u.MaxConcurrentExams,
		&u.MaxStorageSize, &u.WhatsappNumber, &u.Email, &u.ExpiresAt, &u.OTPCode, &u.OTPExpiry,
		&u.Package, &u.BaseRole, &u.PackageRole,
	)
	return u, err
}

func scanAdminUserFromRows(rows pgx.Rows) (AdminUser, error) {
	return scanAdminUser(rows)
}

// GetUserByUsername retrieves a user by username (case-sensitive).
func GetUserByUsername(ctx context.Context, pool *pgxpool.Pool, username string) (AdminUser, error) {
	// Case-insensitive lookup: usernames are stored lowercase for new accounts,
	// but legacy rows may be mixed-case. This keeps login working regardless of
	// the case typed and makes uniqueness checks case-insensitive. A
	// deterministic tie-break (prefer an exact-case match, then lowest id)
	// ensures a stable row when legacy case-variant duplicates exist.
	sql := `SELECT ` + DefaultAdminUserColumns + ` FROM admin_users WHERE LOWER(username) = LOWER($1)
		ORDER BY (username = $1) DESC, id ASC LIMIT 1`
	return scanAdminUser(pool.QueryRow(ctx, sql, username))
}

// GetUserByID retrieves a user by primary key.
func GetUserByID(ctx context.Context, pool *pgxpool.Pool, id int) (AdminUser, error) {
	sql := `SELECT ` + DefaultAdminUserColumns + ` FROM admin_users WHERE id = $1`
	return scanAdminUser(pool.QueryRow(ctx, sql, id))
}

// ListUsersOpts holds optional filters for listing users.
type ListUsersOpts struct {
	Page              int
	PerPage           int
	Search            string
	Instansi          string // operator instansi filter
	RoleFilter        string // optional: "guru", "pengawas", "operator"
	ExcludeSuperAdmin bool
	ExcludeOperator   bool
}

// UserWithExamCount extends AdminUser with the count of exams they created.
type UserWithExamCount struct {
	AdminUser
	ExamCount int `json:"exam_count"`
}

// ListUsersResult holds the paginated user list and total count.
type ListUsersResult struct {
	Users      []UserWithExamCount
	Total      int
	TotalPages int
	Page       int
	PerPage    int
}

// ListUsers returns a paginated list of users, with optional search and instansi filtering.
// The super admin is always excluded unless explicitly searched.
func ListUsers(ctx context.Context, pool *pgxpool.Pool, opts ListUsersOpts) (ListUsersResult, error) {
	if opts.Page < 1 {
		opts.Page = 1
	}
	perPage := clampPerPage(opts.PerPage)

	var conditions []string
	var args []interface{}
	argIdx := 1

	// Exclusion for super admin.
	if opts.ExcludeSuperAdmin {
		conditions = append(conditions, fmt.Sprintf(`u.username != $%d`, argIdx))
		args = append(args, SuperAdminUsername)
		argIdx++
	}

	// Exclusion for operator users.
	if opts.ExcludeOperator {
		conditions = append(conditions, fmt.Sprintf(`u.role NOT ILIKE $%d`, argIdx))
		args = append(args, `%"operator"%`)
		argIdx++
	}

	// Instansi filter (for operator viewing their own instansi).
	if opts.Instansi != "" {
		conditions = append(conditions, fmt.Sprintf(`u.instansi = $%d`, argIdx))
		args = append(args, opts.Instansi)
		argIdx++
	}

	// Role filter (ILIKE on JSON array string like `["guru"]`).
	if opts.RoleFilter != "" {
		conditions = append(conditions, fmt.Sprintf(`u.role ILIKE $%d`, argIdx))
		args = append(args, `%`+opts.RoleFilter+`%`)
		argIdx++
	}

	// Search filter.
	if opts.Search != "" {
		pat := "%" + opts.Search + "%"
		conditions = append(conditions,
			fmt.Sprintf(`(u.username ILIKE $%d OR u.name ILIKE $%d OR u.whatsapp_number ILIKE $%d OR u.email ILIKE $%d)`, argIdx, argIdx+1, argIdx+2, argIdx+3))
		args = append(args, pat, pat, pat, pat)
		argIdx += 4
	}

	// Count query.
	whereClause := ""
	if len(conditions) > 0 {
		whereClause = " WHERE " + joinConditions(conditions, " AND ")
	}

	countSQL := `SELECT COUNT(*) FROM admin_users u` + whereClause
	var total int
	err := pool.QueryRow(ctx, countSQL, args...).Scan(&total)
	if err != nil {
		return ListUsersResult{}, fmt.Errorf("count users: %w", err)
	}

	totalPages := calcTotalPages(total, perPage)
	if opts.Page > totalPages && total > 0 {
		opts.Page = totalPages
	}
	offset := calcOffset(opts.Page, perPage)

	// Data query.
	sql := `SELECT u.id, u.username, u.name, u.password_hash, u.created_at, u.status,
	u.instansi, u.role, u.max_exams, u.max_pdf_size, u.max_concurrent_exams,
	u.max_storage_size, u.whatsapp_number, u.email, u.expires_at, u.otp_code, u.otp_expiry,
	u.base_role, u.package_role,
	COALESCE(u.package, 'free'),
	COALESCE(COUNT(e.id), 0) as exam_count
	FROM admin_users u
	LEFT JOIN exams e ON e.created_by = u.id` + whereClause +
		` GROUP BY u.id ORDER BY 
			CASE
				WHEN u.role ILIKE '%"superadmin"%' THEN 0
				WHEN u.role ILIKE '%"operator"%' THEN 1
				WHEN u.role ILIKE '%"guru"%' THEN 2
				WHEN u.role ILIKE '%"pengawas"%' THEN 3
				ELSE 4
			END, u.username ASC LIMIT $` + fmt.Sprintf("%d", argIdx) +
		` OFFSET $` + fmt.Sprintf("%d", argIdx+1)
	args = append(args, perPage, offset)

	rows, err := pool.Query(ctx, sql, args...)
	if err != nil {
		return ListUsersResult{}, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var users []UserWithExamCount
	for rows.Next() {
		var u AdminUser
		var examCount int
		err := rows.Scan(
			&u.ID, &u.Username, &u.Name, &u.PasswordHash, &u.CreatedAt, &u.Status,
			&u.Instansi, &u.Role, &u.MaxExams, &u.MaxPDFSize, &u.MaxConcurrentExams,
			&u.MaxStorageSize, &u.WhatsappNumber, &u.Email, &u.ExpiresAt, &u.OTPCode, &u.OTPExpiry,
			&u.BaseRole, &u.PackageRole,
			&u.Package,
			&examCount,
		)
		if err != nil {
			return ListUsersResult{}, fmt.Errorf("scan user row: %w", err)
		}
		users = append(users, UserWithExamCount{AdminUser: u, ExamCount: examCount})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Printf("rows iteration error: %v", err)
	}

	if users == nil {
		users = []UserWithExamCount{}
	}

	return ListUsersResult{
		Users:      users,
		Total:      total,
		TotalPages: totalPages,
		Page:       opts.Page,
		PerPage:    perPage,
	}, nil
}

// rowQuerier abstracts a single-query source (pgx.Tx or pgxpool.Pool) so the
// per-IP registration query can be unit-tested without a live database.
type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// CountRecentRegistrationsByIP returns how many accounts were registered from
// the given IP within the last 24 hours. It backs the per-IP registration cap
// (admin_users.registered_ip), a defense-in-depth layer against
// mass-registration on top of Cloudflare Turnstile.
func CountRecentRegistrationsByIP(ctx context.Context, q rowQuerier, ip string) (int, error) {
	var n int
	err := q.QueryRow(ctx,
		`SELECT COUNT(*) FROM admin_users WHERE registered_ip = $1 AND created_at > now() - interval '24 hours'`,
		ip).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count recent registrations by IP: %w", err)
	}
	return n, nil
}

// RegistrationAllowedByPerIPLimit reports whether a new registration from an IP
// is permitted given how many accounts were already registered from that IP in
// the last 24 hours and the configured cap. A cap <= 0 means unlimited.
func RegistrationAllowedByPerIPLimit(recentRegistrations, maxPerIP int) bool {
	if maxPerIP <= 0 {
		return true
	}
	return recentRegistrations < maxPerIP
}

// CreateUser inserts a new admin_users row. The password is hashed with bcrypt.
// Returns the created AdminUser with its generated ID.
func CreateUser(ctx context.Context, pool *pgxpool.Pool, u *AdminUser) (*AdminUser, error) {
	hash, err := HashPassword(u.PasswordHash)
	if err != nil {
		return nil, err
	}

	if u.Package == "" {
		u.Package = "free"
	}

	sql := `INSERT INTO admin_users
	(username, name, password_hash, status, instansi, role, max_exams, max_pdf_size,
	 max_concurrent_exams, max_storage_size, whatsapp_number, email, expires_at, otp_code, otp_expiry, package, registered_ip)
	VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
	RETURNING ` + DefaultAdminUserColumns

	created, err := scanAdminUser(pool.QueryRow(ctx, sql,
		u.Username, u.Name, hash, u.Status, u.Instansi, u.Role,
		u.MaxExams, u.MaxPDFSize, u.MaxConcurrentExams, u.MaxStorageSize,
		u.WhatsappNumber, u.Email, u.ExpiresAt, u.OTPCode, u.OTPExpiry,
		u.Package, u.RegisteredIP,
	))
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return &created, nil
}

// allowedUserColumns is a whitelist of columns that can be updated dynamically.
// This prevents SQL injection via column names in UpdateUserField and UpdateUser.
var allowedUserColumns = map[string]bool{
	"name":                 true,
	"password_hash":        true,
	"status":               true,
	"instansi":             true,
	"role":                 true,
	"base_role":            true,
	"package_role":         true,
	"max_exams":            true,
	"max_pdf_size":         true,
	"max_concurrent_exams": true,
	"max_storage_size":     true,
	"whatsapp_number":      true,
	"email":                true,
	"expires_at":           true,
	"suspended_at":         true,
	"otp_code":             true,
	"otp_expiry":           true,
	"package":              true,
}

// UpdateUserField updates a single column on the admin_users table.
func UpdateUserField(ctx context.Context, pool *pgxpool.Pool, userID int, column string, value interface{}) error {
	if !allowedUserColumns[column] {
		return fmt.Errorf("update user field: disallowed column %q", column)
	}
	// Hash password before storing
	val := value
	if column == "password_hash" {
		if pw, ok := value.(string); ok && pw != "" {
			hash, err := HashPassword(pw)
			if err != nil {
				return fmt.Errorf("hash password: %w", err)
			}
			val = hash
		}
	}
	sql := fmt.Sprintf(`UPDATE admin_users SET %s = $1 WHERE id = $2`, column)
	_, err := pool.Exec(ctx, sql, val, userID)
	if err != nil {
		return fmt.Errorf("update user field %s: %w", column, err)
	}
	return nil
}

// UpdateUser updates multiple fields of an admin_users record.
// Fields are only updated when the pointer is non-nil or value is non-zero for simple types.
func UpdateUser(ctx context.Context, pool *pgxpool.Pool, userID int, updates map[string]interface{}) error {
	if len(updates) == 0 {
		return nil
	}
	// Build SET clause with column whitelist to prevent SQL injection.
	setClause := ""
	args := make([]interface{}, 0, len(updates)+1)
	idx := 1
	for col, val := range updates {
		if !allowedUserColumns[col] {
			return fmt.Errorf("update user: disallowed column %q", col)
		}
		if idx > 1 {
			setClause += ", "
		}
		setClause += fmt.Sprintf("%s = $%d", col, idx)
		// Hash password before storing.
		if col == "password_hash" {
			if pw, ok := val.(string); ok {
				hash, err := HashPassword(pw)
				if err != nil {
					return err
				}
				args = append(args, hash)
				idx++
				continue
			}
		}
		args = append(args, val)
		idx++
	}

	sql := fmt.Sprintf(`UPDATE admin_users SET %s WHERE id = $%d`, setClause, idx)
	args = append(args, userID)

	_, err := pool.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("update user: %w", err)
	}
	return nil
}

// ToggleUserStatus switches the user status between 'active' and 'suspended'.
// When activating an expired user, a new expiry date of +1 day is set.
// Returns the new status and an optional message.
func ToggleUserStatus(ctx context.Context, pool *pgxpool.Pool, userID int) (string, string, error) {
	user, err := GetUserByID(ctx, pool, userID)
	if err != nil {
		return "", "", fmt.Errorf("toggle user status: get user: %w", err)
	}

	var newStatus string
	var msg string

	if user.Status == UserStatusActive {
		newStatus = UserStatusSuspended
		msg = fmt.Sprintf("User \"%s\" dinonaktifkan", user.Username)
	} else {
		newStatus = UserStatusActive
		if user.ExpiresAt == nil {
			// No expiry set: add +1 day from now. This explicit renewal grant
			// supersedes the suspension freeze (no frozen remaining exists).
			newExp := time.Now().UTC().Add(24 * time.Hour)
			_, execErr := pool.Exec(ctx,
				`UPDATE admin_users SET status = $1, expires_at = $2, suspended_at = NULL WHERE id = $3`,
				newStatus, newExp, userID)
			if execErr != nil {
				return "", "", fmt.Errorf("toggle user status update with default expiry: %w", execErr)
			}
			// Keep the active package's clock aligned with the renewed expiry so
			// the billing display and future pause computations stay accurate.
			if err := SyncActiveRedemptionToExpiry(ctx, pool, userID, newExp); err != nil {
				log.Printf("toggle user status: sync redemption expiry error: %v", err)
			}
			msg = fmt.Sprintf("User \"%s\" diaktifkan. Masa aktif: +1 hari", user.Username)
			return newStatus, msg, nil
		}
		if user.ExpiresAt.Before(time.Now().UTC()) {
			// Expired: renew with +1 day. This explicit renewal grant supersedes
			// the suspension freeze — the admin deliberately grants a fresh
			// period instead of restoring the remaining-at-suspension time.
			newExp := time.Now().UTC().Add(24 * time.Hour)
			_, execErr := pool.Exec(ctx,
				`UPDATE admin_users SET status = $1, expires_at = $2, suspended_at = NULL WHERE id = $3`,
				newStatus, newExp, userID)
			if execErr != nil {
				return "", "", fmt.Errorf("toggle user status update with expiry: %w", execErr)
			}
			// Keep the active package's clock aligned with the renewed expiry so
			// the billing display and future pause computations stay accurate.
			if err := SyncActiveRedemptionToExpiry(ctx, pool, userID, newExp); err != nil {
				log.Printf("toggle user status: sync redemption expiry error: %v", err)
			}
			msg = fmt.Sprintf("User \"%s\" diaktifkan. Masa aktif: +1 hari (expired)", user.Username)
			return newStatus, msg, nil
		}

		// Active with valid future expiry: activate without changing expiry,
		// but freeze the account clock for the suspension period so the
		// package lifetime did not burn while the user was locked out.
		extendedExpiry, err := ResumeSuspendedAccountClock(ctx, pool, userID)
		if err != nil {
			return "", "", fmt.Errorf("toggle user status reactivate: %w", err)
		}
		msg = fmt.Sprintf("User \"%s\" diaktifkan (masa aktif dipertahankan)", user.Username)
		if extendedExpiry != nil {
			msg = fmt.Sprintf("User \"%s\" diaktifkan. Masa aktif diperpanjang sampai %s (jam dijeda selama suspend)",
				user.Username, extendedExpiry.Format("2006-01-02 15:04:05"))
		}
		return newStatus, msg, nil
	}

	// Suspending: record when it started so reactivation can freeze the
	// account clock for the suspension period.
	_, err = pool.Exec(ctx, `UPDATE admin_users SET status = $1, suspended_at = now() WHERE id = $2`, newStatus, userID)
	if err != nil {
		return "", "", fmt.Errorf("toggle user status update: %w", err)
	}
	return newStatus, msg, nil
}

// ResumeSuspendedAccountClock reactivates the account clock after a
// suspension: the account's expires_at is extended by the suspension duration
// (so the package lifetime did not burn while the user was locked out),
// suspended_at is cleared, and the active redemption's clock is realigned to
// the extended expiry. No-op freeze when the account was never suspended or
// has no expiry (the marker is still cleared). Returns the resulting expiry
// (nil when unchanged).
func ResumeSuspendedAccountClock(ctx context.Context, pool *pgxpool.Pool, userID int) (*time.Time, error) {
	var suspendedAt *time.Time
	var expiresAt *time.Time
	if err := pool.QueryRow(ctx,
		`SELECT suspended_at, expires_at FROM admin_users WHERE id = $1`, userID).Scan(&suspendedAt, &expiresAt); err != nil {
		return nil, err
	}

	if suspendedAt == nil || expiresAt == nil || !suspendedAt.Before(time.Now().UTC()) {
		// Nothing to freeze (never suspended, no expiry, or clock skew): just
		// make sure the account is active and the marker is gone.
		if _, err := pool.Exec(ctx,
			`UPDATE admin_users SET status = $1, suspended_at = NULL WHERE id = $2`,
			UserStatusActive, userID); err != nil {
			return nil, err
		}
		return nil, nil
	}

	newExpiry := expiresAt.Add(time.Since(*suspendedAt))
	if _, err := pool.Exec(ctx,
		`UPDATE admin_users SET status = $1, expires_at = $2, suspended_at = NULL WHERE id = $3`,
		UserStatusActive, newExpiry, userID); err != nil {
		return nil, err
	}
	// Realign the active package's clock so future pause computations and the
	// billing display match the (frozen) account expiry.
	if err := SyncActiveRedemptionToExpiry(ctx, pool, userID, newExpiry); err != nil {
		return nil, err
	}
	return &newExpiry, nil
}

// DeleteUser deletes a user and returns their ID and the file paths of their exams
// so the caller can clean up stored files. The caller should also remove related
// exam_pengawas entries (DB cascade handles this).
func DeleteUser(ctx context.Context, pool *pgxpool.Pool, userID int) ([]string, error) {
	// Start DB transaction
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("delete user: begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	// Lock target user row
	var target AdminUser
	err = tx.QueryRow(ctx,
		`SELECT id, username, instansi, role FROM admin_users WHERE id = $1 FOR UPDATE`,
		userID,
	).Scan(&target.ID, &target.Username, &target.Instansi, &target.Role)
	if err != nil {
		return nil, fmt.Errorf("delete user: lookup target: %w", err)
	}

	var cascadedIDs []int
	if target.HasRole(RoleOperator) && target.Instansi != "" {
		rows, err := tx.Query(ctx,
			`SELECT id FROM admin_users WHERE instansi = $1 AND id != $2 FOR UPDATE`,
			target.Instansi, userID)
		if err != nil {
			return nil, fmt.Errorf("delete user: query instansi users: %w", err)
		}
		for rows.Next() {
			var cid int
			if err := rows.Scan(&cid); err != nil {
				rows.Close()
				return nil, fmt.Errorf("delete user: scan cascaded id: %w", err)
			}
			cascadedIDs = append(cascadedIDs, cid)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("delete user: rows err: %w", err)
		}
	}

	// Collect all IDs being deleted (cascaded users + target).
	allIDs := append(cascadedIDs, userID)

	// Collect file paths from all exams created by any of these users.
	var paths []string
	for _, uid := range allIDs {
		rows, err := tx.Query(ctx, `SELECT file_path FROM exams WHERE created_by = $1`, uid)
		if err != nil {
			return nil, fmt.Errorf("delete user: query exams: %w", err)
		}
		for rows.Next() {
			var fp string
			if err := rows.Scan(&fp); err != nil {
				rows.Close()
				return nil, fmt.Errorf("delete user: scan file_path: %w", err)
			}
			paths = append(paths, fp)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			log.Printf("rows iteration error: %v", err)
		}
	}

	// Nullify delegated_to references so FK doesn't block the delete.
	for _, uid := range allIDs {
		if _, err := tx.Exec(ctx, `UPDATE exams SET delegated_to = NULL WHERE delegated_to = $1`, uid); err != nil {
			return nil, fmt.Errorf("delete user: clear delegated_to: %w", err)
		}
	}

	// Delete exams created by all users being deleted.
	for _, uid := range allIDs {
		if _, err := tx.Exec(ctx, `DELETE FROM exams WHERE created_by = $1`, uid); err != nil {
			return nil, fmt.Errorf("delete user: delete exams: %w", err)
		}
	}

	// Delete cascaded users first (exam_pengawas cascades via DB).
	for _, cid := range cascadedIDs {
		if _, err := tx.Exec(ctx, `DELETE FROM admin_users WHERE id = $1`, cid); err != nil {
			return nil, fmt.Errorf("delete cascaded user %d error: %w", cid, err)
		}
	}

	// Finally delete the target user.
	_, err = tx.Exec(ctx, `DELETE FROM admin_users WHERE id = $1`, userID)
	if err != nil {
		return nil, fmt.Errorf("delete user: exec: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("delete user: commit transaction: %w", err)
	}

	return paths, nil
}

// ListUsersForInstansi returns active users belonging to a specific instansi,
// optionally filtered by role pattern.
type ListForInstansiOpts struct {
	Instansi  string
	RoleLike  string // optional role pattern e.g. '%"pengawas"%'
	ExcludeID int    // optional user ID to exclude
}

func ListUsersForInstansi(ctx context.Context, pool *pgxpool.Pool, opts ListForInstansiOpts) ([]AdminUser, error) {
	var conditions []string
	var args []interface{}
	argIdx := 1

	conditions = append(conditions, fmt.Sprintf(`instansi = $%d`, argIdx))
	args = append(args, opts.Instansi)
	argIdx++

	conditions = append(conditions, fmt.Sprintf(`status = $%d`, argIdx))
	args = append(args, UserStatusActive)
	argIdx++

	if opts.RoleLike != "" {
		conditions = append(conditions, fmt.Sprintf(`role ILIKE $%d`, argIdx))
		args = append(args, opts.RoleLike)
		argIdx++
	}

	if opts.ExcludeID > 0 {
		conditions = append(conditions, fmt.Sprintf(`id != $%d`, argIdx))
		args = append(args, opts.ExcludeID)
		argIdx++
	}

	whereClause := " WHERE " + joinConditions(conditions, " AND ")

	sql := `SELECT ` + DefaultAdminUserColumns + ` FROM admin_users` + whereClause + ` ORDER BY username ASC`
	rows, err := pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("list users for instansi: %w", err)
	}
	defer rows.Close()

	var users []AdminUser
	for rows.Next() {
		u, err := scanAdminUserFromRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scan user row: %w", err)
		}
		users = append(users, u)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Printf("rows iteration error: %v", err)
	}

	if users == nil {
		users = []AdminUser{}
	}
	return users, nil
}

// AuthenticateUser verifies credentials and returns the user if valid.
// Returns nil user and an error message if authentication fails.
func AuthenticateUser(ctx context.Context, pool *pgxpool.Pool, username, password string) (*AdminUser, string) {
	user, err := GetUserByUsername(ctx, pool, username)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, "Username atau password salah"
		}
		return nil, "Terjadi kesalahan. Silakan coba lagi."
	}

	if !CheckPassword(password, user.PasswordHash) {
		return nil, "Username atau password salah"
	}

	if user.Status == UserStatusSuspended {
		return nil, "Akun Anda telah dinonaktifkan oleh administrator."
	}

	if user.Status == UserStatusPendingOTP {
		return nil, "Pendaftaran Anda membutuhkan konfirmasi OTP Email. Silakan cek email Anda."
	}

	if user.IsExpired() {
		// A user whose account expiry has passed may still hold a claimed
		// voucher with remaining lifetime that they could activate (the
		// background auto-fallback job may not have run yet, or the server was
		// down when the package expired). Let them in so they can pick a
		// package on the billing page; otherwise they would be locked out
		// despite owning a usable package.
		var usable int
		if err := pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM voucher_redemptions
			 WHERE user_id = $1 AND NOT is_active AND remaining_seconds > 0`,
			user.ID).Scan(&usable); err != nil || usable == 0 {
			return nil, "Masa aktif akun Anda telah habis. Silakan hubungi administrator."
		}
	}

	return &user, ""
}

// VerifyUserManual activates a pending user and clears OTP fields.
func VerifyUserManual(ctx context.Context, pool *pgxpool.Pool, userID int) error {
	_, err := pool.Exec(ctx,
		`UPDATE admin_users SET status = 'active', otp_code = NULL, otp_expiry = NULL WHERE id = $1`,
		userID)
	if err != nil {
		return fmt.Errorf("verify user: %w", err)
	}
	return nil
}

// SuperAdminUsername holds the expected super admin username from config.
// Set this at application startup.
var SuperAdminUsername = "superadmin"

// DaysUntilExpiry calculates the number of days remaining before account expiry.
func DaysUntilExpiry(expiresAtStr *string) *int {
	if expiresAtStr == nil || *expiresAtStr == "" {
		return nil
	}
	exp, err := time.Parse("2006-01-02 15:04:05", *expiresAtStr)
	if err != nil {
		return nil
	}
	remaining := int(math.Ceil(exp.Sub(time.Now().UTC()).Hours() / 24))
	if remaining < 0 {
		remaining = 0
	}
	return &remaining
}
