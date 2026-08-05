package admin

import (
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/examvan/webui/internal/models"
)

// TestExportSingleExamWorkbookDetailSheets builds a workbook the same way
// exportSingleExamXLSX does (summary sheet + one detail sheet per student)
// and verifies the sheet list and cell contents. It guards against the
// workbook structure regressing, e.g. when the detail sheets are removed.
func TestExportSingleExamWorkbookDetailSheets(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()
	st := newXLSXStyles(f)

	summary := "Rekapitulasi"
	if err := f.SetSheetName("Sheet1", summary); err != nil {
		t.Fatalf("rename sheet: %v", err)
	}

	start := "2026-08-05 08:00:00"
	score := 87.5
	questions := []models.Question{
		{Number: 1, Type: "single_choice", Weight: 1, Key: "A"},
		{Number: 2, Type: "short_answer", Weight: 2, Key: "Jakarta"},
	}
	subs := []models.Submission{
		{ID: 1, ExamID: 9, StudentName: "Budi Santoso", ExamNumber: "001", StudentClass: "XII-A",
			Score: &score, StartTime: &start, CreatedAt: time.Now(), MACAddress: "AA:BB:CC:DD:EE:FF"},
		{ID: 2, ExamID: 9, StudentName: "Siti Aminah", ExamNumber: "002", StudentClass: "XII-A",
			Score: nil, StartTime: nil, CreatedAt: time.Now(), MACAddress: "11:22:33:44:55:66"},
		// Case-insensitive duplicate of student 1 — must get a numbered suffix
		// because Excel sheet names are case-insensitive.
		{ID: 3, ExamID: 9, StudentName: "budi santoso", ExamNumber: "003", StudentClass: "XII-A",
			Score: nil, StartTime: nil, CreatedAt: time.Now(), MACAddress: "99:88:77:66:55:44"},
	}

	used := map[string]int{}
	for i, sub := range subs {
		row := i + 2
		_ = f.SetCellInt(summary, cellRef(1, row), int64(sub.ID))
		_ = f.SetCellStr(summary, cellRef(3, row), sub.StudentName)

		answers := map[string]interface{}{"1": "A", "2": "Jakarta"}
		evaluated := models.EvaluateAnswersDetailed(answers, questions)

		base := sanitizeSheetName("Detail - " + sub.StudentName)
		name := base
		key := strings.ToLower(base)
		if used[key] > 0 {
			name = base + " (2)"
		}
		used[key]++
		if _, err := f.NewSheet(name); err != nil {
			t.Fatalf("new sheet %q: %v", name, err)
		}
		writeStudentDetailSheet(f, name, "Ujian Matematika", sub, answers, evaluated, questions, st)
	}

	sheets := f.GetSheetList()
	if len(sheets) != 4 {
		t.Fatalf("expected 4 sheets (summary + 3 details), got %v", sheets)
	}
	if sheets[0] != "Rekapitulasi" {
		t.Errorf("first sheet should be %q, got %q", summary, sheets[0])
	}
	if !contains(sheets, "Detail - Budi Santoso") || !contains(sheets, "Detail - Siti Aminah") || !contains(sheets, "Detail - budi santoso (2)") {
		t.Errorf("missing per-student detail sheets, got %v", sheets)
	}

	// Metadata: Nama Siswa label at A3, value at B3.
	if v, _ := f.GetCellValue("Detail - Budi Santoso", "B3"); v != "Budi Santoso" {
		t.Errorf("expected student name in B3, got %q", v)
	}
	// Question 1 header at row 11 (8 meta rows + spacer), answer row 12:
	// D12 = student answer "A", E12 = key "A", F12 = status "correct".
	if v, _ := f.GetCellValue("Detail - Budi Santoso", "D12"); v != "A" {
		t.Errorf("expected answer A in D12, got %q", v)
	}
	if v, _ := f.GetCellValue("Detail - Budi Santoso", "F12"); v != "correct" {
		t.Errorf("expected status correct in F12, got %q", v)
	}

	// Sanitization: invalid sheet characters replaced, length capped at 31.
	if got := sanitizeSheetName(`Budi:Santoso/1`); got != "Budi_Santoso_1" {
		t.Errorf("sanitizeSheetName mismatch: %q", got)
	}
	if got := sanitizeSheetName(""); got != "Siswa" {
		t.Errorf("sanitizeSheetName empty fallback mismatch: %q", got)
	}
	if got := sanitizeSheetName("'Budi'"); got != "Budi" {
		t.Errorf("sanitizeSheetName should trim apostrophes, got %q", got)
	}
	long := sanitizeSheetName(strings.Repeat("Nama Panjang ", 5))
	if len([]rune(long)) > 31 {
		t.Errorf("sanitizeSheetName should cap at 31 chars, got %d: %q", len([]rune(long)), long)
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
