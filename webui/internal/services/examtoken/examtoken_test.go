package examtoken

import (
	"testing"

	"github.com/examvan/webui/internal/models"
)

func TestMatchesDynamicUsesActiveTokenOnly(t *testing.T) {
	mode := "dynamic"
	exam := models.Exam{Token: "STATIC123", ActiveToken: "ACTIVE123", TokenMode: &mode}

	if !Matches(exam, "ACTIVE123") {
		t.Fatal("expected active token to match in dynamic mode")
	}
	if Matches(exam, "STATIC123") {
		t.Fatal("did not expect permanent token to match in dynamic mode")
	}
}

func TestMatchesStaticAllowsLegacyFallback(t *testing.T) {
	mode := "static"
	exam := models.Exam{Token: "STATIC123", ActiveToken: "", TokenMode: &mode}

	if !Matches(exam, "STATIC123") {
		t.Fatal("expected permanent token fallback to match in static mode")
	}
}

func TestMatchesRejectsEmptyToken(t *testing.T) {
	exam := models.Exam{Token: "STATIC123", ActiveToken: "ACTIVE123"}
	if Matches(exam, "") {
		t.Fatal("expected empty token to be rejected")
	}
}
