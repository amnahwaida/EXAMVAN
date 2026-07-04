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

	lastReset := *exam.ExamStartedAt
	if exam.TokenLastResetAt != nil {
		lastReset = *exam.TokenLastResetAt
	}
	if now.UTC().Before(lastReset.Add(time.Duration(*exam.TokenResetInterval) * time.Minute)) {
		return nil
	}

	newToken := helpers.GenerateExamToken()
	if err := models.UpdateExamActiveToken(ctx, pool, exam.ID, newToken); err != nil {
		return err
	}
	exam.ActiveToken = newToken
	resetAt := now.UTC()
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
