package admin

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	// slowUploadThreshold is deliberately below Cloudflare's fixed ~100s
	// origin-response timeout (524). Once an upload passes this point we send
	// response headers and heartbeats so the edge does not abort the request.
	slowUploadThreshold = 60 * time.Second
	// uploadHeartbeatInterval must stay well below the 100s edge idle window.
	uploadHeartbeatInterval = 15 * time.Second
)

type slowOperationResult struct {
	status  int
	payload gin.H
}

// respondSlowOperation runs operation and returns its normal HTTP status when
// it finishes quickly. If it is still running at threshold, the response is
// switched to a streamed 200 with whitespace heartbeats; Cloudflare counts
// time-to-first-byte, so this prevents a 524 while R2 uploads a large file.
// The final JSON still says success=false on error, which the admin UI handles.
func respondSlowOperation(c *gin.Context, threshold time.Duration, operation func(context.Context) (int, gin.H)) {
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	result := make(chan slowOperationResult, 1)
	go func() {
		status, payload := operation(ctx)
		result <- slowOperationResult{status: status, payload: payload}
	}()

	timer := time.NewTimer(threshold)
	defer timer.Stop()

	select {
	case res := <-result:
		c.JSON(res.status, res.payload)
		return
	case <-ctx.Done():
		return
	case <-timer.C:
	}

	// Slow path: send headers immediately and keep bytes flowing. The leading
	// whitespace is valid around the final JSON document.
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	c.Writer.WriteHeaderNow()
	if _, err := c.Writer.Write([]byte(" ")); err != nil {
		return
	}
	c.Writer.Flush()

	ticker := time.NewTicker(uploadHeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case res := <-result:
			c.JSON(http.StatusOK, res.payload)
			return
		case <-ctx.Done():
			// Headers are already 200; still terminate the streamed body with a
			// parseable JSON error instead of leaving the client hanging.
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "Unggahan terputus. Silakan coba lagi."})
			return
		case <-ticker.C:
			if _, err := c.Writer.Write([]byte(" ")); err != nil {
				return
			}
			c.Writer.Flush()
		}
	}
}
