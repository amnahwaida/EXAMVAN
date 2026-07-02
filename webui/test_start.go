package main

import (
	"context"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	dbURL := "postgres://examvan:examvan2026@localhost:5432/examvan?sslmode=disable"
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	id := 1
	res, err := pool.Exec(context.Background(), `UPDATE exams SET status = 'active', exam_started_at = CURRENT_TIMESTAMP, token_last_reset_at = CURRENT_TIMESTAMP WHERE id = $1`, id)
	if err != nil {
		log.Fatalf("SQL error: %v", err)
	}
	fmt.Printf("Updated %d rows\n", res.RowsAffected())
}
