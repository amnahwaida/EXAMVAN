package api

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	redis "github.com/redis/go-redis/v9"

	"github.com/examvan/webui/internal/config"
	"github.com/examvan/webui/internal/handlers/admin"
	"github.com/examvan/webui/internal/models"
	"github.com/examvan/webui/internal/payment"
)

// processedRequests guards against replay attacks — in-memory, lost on restart.
// Good enough: webhook replay window is minutes, not months.
var (
	processedRequests sync.Map
	replayTTL         = 10 * time.Minute
)

func markDokuRequestProcessed(ctx context.Context, rdb *redis.Client, requestID string) (bool, error) {
	if rdb != nil {
		redisKey := "doku:webhook:request_id:" + requestID
		set, err := rdb.SetNX(ctx, redisKey, "processed", replayTTL).Result()
		if err != nil {
			return false, err
		}
		return !set, nil
	}
	if _, loaded := processedRequests.LoadOrStore(requestID, time.Now().UTC()); loaded {
		return true, nil
	}
	return false, nil
}

func cleanupStaleDokuRequestMarkers() {
	processedRequests.Range(func(key, val interface{}) bool {
		if t, ok := val.(time.Time); ok && time.Since(t) > replayTTL {
			processedRequests.Delete(key)
		}
		return true
	})
}

type DokuNotification struct {
	Order struct {
		InvoiceNumber string `json:"invoice_number"`
		Amount        int64  `json:"amount"`
	} `json:"order"`
	Transaction struct {
		Status string `json:"status"`
	} `json:"transaction"`
}

// DokuNotifyHandler processes incoming DOKU webhooks to automatically approve transactions
func DokuNotifyHandler(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		pool := getPool(c)
		if pool == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Database pool not found"})
			return
		}
		ctx := c.Request.Context()

		var rdb *redis.Client
		if r, exists := c.Get("redis"); exists && r != nil {
			rdb = r.(*redis.Client)
		}

		// 1. Read request body bytes to verify signature
		bodyBytes, err := io.ReadAll(c.Request.Body)
		if err != nil {
			log.Printf("Doku Webhook: failed to read body: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Failed to read body"})
			return
		}
		// Restore body reader
		c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

		// 2. Security Headers
		clientIDHeader := c.GetHeader("Client-Id")
		requestIDHeader := c.GetHeader("Request-Id")
		timestampHeader := c.GetHeader("Request-Timestamp")
		signatureHeader := c.GetHeader("Signature")
		if signatureHeader == "" {
			signatureHeader = c.GetHeader("X-Signature")
		}

		if clientIDHeader == "" || requestIDHeader == "" || timestampHeader == "" || signatureHeader == "" {
			log.Printf("Doku Webhook: missing security headers")
			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "Missing security headers"})
			return
		}

		// 2a. Validate Client-Id matches configured value
		if clientIDHeader != cfg.DokuClientID {
			log.Printf("Doku Webhook: Client-Id mismatch. Got: %s, Expected: %s", clientIDHeader, cfg.DokuClientID)
			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "Invalid Client-Id"})
			return
		}

		// 2b. Validate Request-Timestamp is within 5 minutes
		ts, err := time.Parse(time.RFC3339, timestampHeader)
		if err != nil {
			log.Printf("Doku Webhook: invalid timestamp format: %s", timestampHeader)
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid timestamp format"})
			return
		}
		now := time.Now().UTC()
		diff := now.Sub(ts)
		if diff < 0 {
			diff = -diff
		}
		if diff > 5*time.Minute {
			log.Printf("Doku Webhook: timestamp too old/advanced: %s (diff=%v)", timestampHeader, diff)
			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "Timestamp outside allowed window"})
			return
		}

		// 3. Signature Validation (after Client-Id confirmed)
		digest := payment.CalculateDigest(bodyBytes)
		targetPath := c.Request.URL.Path
		computedSignature := payment.GenerateDokuSignature(clientIDHeader, requestIDHeader, timestampHeader, targetPath, digest, cfg.DokuSecretKey)

		if computedSignature != signatureHeader {
			log.Printf("Doku Webhook: signature mismatch. Received: %s, Computed: %s", signatureHeader, computedSignature)
			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "Signature verification failed"})
			return
		}

		// 4. Parse JSON Body
		var notification DokuNotification
		if err := c.ShouldBindJSON(&notification); err != nil {
			log.Printf("Doku Webhook: failed to parse JSON payload: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid JSON payload"})
			return
		}

		invoiceNumber := strings.TrimSpace(notification.Order.InvoiceNumber)
		if invoiceNumber == "" {
			log.Printf("Doku Webhook: missing invoice number")
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Missing invoice_number"})
			return
		}

		// Invoice format should be EXAMVAN-TX-<tx_id>
		const prefix = "EXAMVAN-TX-"
		if !strings.HasPrefix(invoiceNumber, prefix) {
			log.Printf("Doku Webhook: invalid invoice number format: %s", invoiceNumber)
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid invoice_number format"})
			return
		}

		txIDStr := strings.TrimPrefix(invoiceNumber, prefix)
		txID, err := strconv.Atoi(txIDStr)
		if err != nil {
			log.Printf("Doku Webhook: failed to parse transaction ID: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid transaction ID"})
			return
		}

		// 5. Fetch transaction from DB to validate amount
		tx, err := models.GetTransactionByID(ctx, pool, txID)
		if err != nil {
			log.Printf("Doku Webhook: transaction %d not found: %v", txID, err)
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "Transaction not found"})
			return
		}
		if tx.Status != models.TxStatusPending {
			log.Printf("Doku Webhook: transaction %d already processed (status=%s)", txID, tx.Status)
			c.JSON(http.StatusOK, gin.H{"success": true, "message": "Transaction already processed"})
			return
		}
		if notification.Order.Amount != tx.Amount {
			log.Printf("Doku Webhook: amount mismatch. Notification: %d, DB: %d", notification.Order.Amount, tx.Amount)
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Amount mismatch"})
			return
		}

		status := strings.ToUpper(notification.Transaction.Status)
		if status != "SUCCESS" && status != "CANCEL" && status != "CANCELLED" && status != "FAILED" && status != "EXPIRED" && status != "REFUND" && status != "VOID" && status != "REVERSAL" {
			log.Printf("Doku Webhook: unknown transaction status: %s", status)
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Unknown transaction status"})
			return
		}

		// 6. Mark request as processed only after validation succeeds
		isDuplicate, err := markDokuRequestProcessed(ctx, rdb, requestIDHeader)
		if err != nil {
			log.Printf("Doku Webhook: replay mark error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Failed to record webhook"})
			return
		}
		if isDuplicate {
			log.Printf("Doku Webhook: duplicate Request-Id: %s", requestIDHeader)
			c.JSON(http.StatusOK, gin.H{"success": true, "message": "Duplicate notification, already processed"})
			return
		}
		cleanupStaleDokuRequestMarkers()

		// 7. Check and process payment status
		if status == "SUCCESS" {
			// Approve transaction in DB
			notes := "Approved automatically via DOKU Payment Gateway (Request-Id: " + requestIDHeader + ")"
			err = admin.ProcessTransactionApproval(ctx, pool, txID, notes)
			if err != nil {
				log.Printf("Doku Webhook: failed to process transaction approval: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Failed to approve transaction"})
				return
			}
			log.Printf("Doku Webhook: transaction %d approved automatically", txID)
		} else if status == "CANCEL" || status == "CANCELLED" || status == "FAILED" || status == "EXPIRED" {
			notes := "Cancelled/Failed automatically via DOKU Payment Gateway (Request-Id: " + requestIDHeader + ", Status: " + status + ")"
			err = admin.ProcessTransactionReversal(ctx, pool, txID, models.TxStatusRejected, notes)
			if err != nil {
				log.Printf("Doku Webhook: failed to process transaction cancellation: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Failed to cancel transaction"})
				return
			}
			log.Printf("Doku Webhook: transaction %d cancelled/rejected (status=%s)", txID, status)
		} else if status == "REFUND" || status == "VOID" || status == "REVERSAL" {
			notes := "Refunded/Voided automatically via DOKU Payment Gateway (Request-Id: " + requestIDHeader + ", Status: " + status + ")"
			err = admin.ProcessTransactionReversal(ctx, pool, txID, models.TxStatusRejected, notes)
			if err != nil {
				log.Printf("Doku Webhook: failed to process transaction reversal: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Failed to reverse transaction"})
				return
			}
			log.Printf("Doku Webhook: transaction %d reversed/refunded (status=%s)", txID, status)
		}

		c.JSON(http.StatusOK, gin.H{"success": true, "message": "Notification processed successfully"})
	}
}
