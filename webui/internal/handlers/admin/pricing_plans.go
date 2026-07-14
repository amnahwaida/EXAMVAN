package admin

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/examvan/webui/internal/models"
)

// ListPricingPlans returns all pricing plans (including inactive) as JSON for admin management.
func ListPricingPlans() gin.HandlerFunc {
	return func(c *gin.Context) {
		pool := getPool(c)
		ctx := c.Request.Context()

		plans, err := models.GetAllPricingPlansAdmin(ctx, pool)
		if err != nil {
			errorResponse(c, http.StatusInternalServerError, "Gagal memuat daftar paket harga.")
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true, "data": plans})
	}
}

// CreatePricingPlan handles creating a new pricing plan.
func CreatePricingPlan() gin.HandlerFunc {
	return func(c *gin.Context) {
		pool := getPool(c)
		ctx := c.Request.Context()

		var plan models.PricingPlan
		if err := c.ShouldBindJSON(&plan); err != nil {
			errorResponse(c, http.StatusBadRequest, "Data tidak valid: "+err.Error())
			return
		}

		if err := models.CreatePricingPlan(ctx, pool, &plan); err != nil {
			errorResponse(c, http.StatusInternalServerError, "Gagal membuat paket harga.")
			return
		}

		successMessage(c, "Paket harga berhasil dibuat.")
	}
}

// UpdatePricingPlan handles updating an existing pricing plan.
func UpdatePricingPlan() gin.HandlerFunc {
	return func(c *gin.Context) {
		pool := getPool(c)
		ctx := c.Request.Context()

		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID tidak valid.")
			return
		}

		var plan models.PricingPlan
		if err := c.ShouldBindJSON(&plan); err != nil {
			errorResponse(c, http.StatusBadRequest, "Data tidak valid: "+err.Error())
			return
		}
		plan.ID = id

		if err := models.UpdatePricingPlan(ctx, pool, &plan); err != nil {
			errorResponse(c, http.StatusInternalServerError, "Gagal memperbarui paket harga.")
			return
		}

		successMessage(c, "Paket harga berhasil diperbarui.")
	}
}

// DeletePricingPlan handles deleting a pricing plan.
func DeletePricingPlan() gin.HandlerFunc {
	return func(c *gin.Context) {
		pool := getPool(c)
		ctx := c.Request.Context()

		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID tidak valid.")
			return
		}

		if err := models.DeletePricingPlan(ctx, pool, id); err != nil {
			errorResponse(c, http.StatusInternalServerError, "Gagal menghapus paket harga.")
			return
		}

		successMessage(c, "Paket harga berhasil dihapus.")
	}
}
