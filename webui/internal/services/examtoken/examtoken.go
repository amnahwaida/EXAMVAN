package examtoken

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/helpers"
	"github.com/examvan/webui/internal/models"
)

// MaybeResetActiveToken rotates active_token for started dynamic exams when the reset interval has elapsed.
func MaybeResetActiveToken(ctx context.Context, pool *pgxpool.Pool, exam *models.Exam, now time.Time) error {
	if exam == nil || pool == nil {
		return nil
	}
	if exam.GetTokenMode() != "dynamic" || exam.ExamStartedAt == nil || exam.TokenResetInterval == nil || *exam.TokenResetInterval <= 0 {
		return nil
	}

	// Open transaction to prevent race conditions during token rotation
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Fetch latest exam token state with row lock
	var examStartedAt *time.Time
	var tokenResetInterval *int
	var tokenLastResetAt *time.Time
	var activeToken string
	err = tx.QueryRow(ctx, 
		`SELECT exam_started_at, token_reset_interval, token_last_reset_at, active_token 
		 FROM exams WHERE id = $1 FOR UPDATE`, exam.ID).Scan(&examStartedAt, &tokenResetInterval, &tokenLastResetAt, &activeToken)
	if err != nil {
		return err
	}

	// Recheck conditions under lock
	if examStartedAt == nil || tokenResetInterval == nil || *tokenResetInterval <= 0 {
		return nil
	}

	lastReset := *examStartedAt
	if tokenLastResetAt != nil {
		lastReset = *tokenLastResetAt
	}
	if now.UTC().Before(lastReset.Add(time.Duration(*tokenResetInterval) * time.Minute)) {
		// Already updated by another concurrent request, sync memory representation and return
		exam.ActiveToken = activeToken
		exam.TokenLastResetAt = tokenLastResetAt
		return nil
	}

	newToken := helpers.GenerateExamToken()
	resetAt := now.UTC()
	_, err = tx.Exec(ctx, 
		`UPDATE exams SET active_token = $1, token_last_reset_at = $2 WHERE id = $3`, 
		newToken, resetAt, exam.ID)
	if err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	exam.ActiveToken = newToken
	exam.TokenLastResetAt = &resetAt
	return nil
}

// Matches reports whether token is currently valid for the exam.
func Matches(exam models.Exam, token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}

	if exam.GetTokenMode() == "dynamic" {
		return exam.ActiveToken != "" && strings.EqualFold(token, exam.ActiveToken)
	}

	if exam.ActiveToken != "" && strings.EqualFold(token, exam.ActiveToken) {
		return true
	}
	return exam.Token != "" && strings.EqualFold(token, exam.Token)
}
