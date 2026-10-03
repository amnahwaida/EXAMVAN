package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"testing"
)

// ---------------------------------------------------------------------------
// M8 (lanjutan) — jalur submit harus lewat `expectedIdentityFields`.
//
// Kenapa tes ini perlu ada
// -----------------------
// `expectedIdentityFields` punya tesnya sendiri (identity_fields_malformed_test.go)
// dan semuanya hijau pada kode submit yang:
//
//	expectedFields := []identityField{}   // atau hasil parse yang error-nya
//	                                       // ditelan dengan `_ =`
//
//(alias lain: `if err := json.Unmarshal(...); err == nil { ... }` yang
// menyisakan `expectedFields` kosong). Kolom `identity_fields` yang rusak
// berarti `expectedFields` kosong, `firstMissingRequiredIdentity` tidak
// mengiterasi apa pun, dan submit TANPA identitas apa pun diterima.
//
// Argumen "helper-nya sudah diuji" tidak menutup celah ini: yang perlu
// dijaga adalah call site-nya, sama seperti H7 di
// handlers/public/hasil_limiter_wiring_test.go.
//
// Invarian: `SubmitExam` memanggil `expectedIdentityFields` tepat sekali, dan
// `firstMissingRequiredIdentity` dipanggil dengan hasil itu.
// ---------------------------------------------------------------------------

var apiFset *token.FileSet

// parseExamsSource mengurai exams.go.
func parseExamsSource(t *testing.T) *ast.File {
	t.Helper()
	apiFset = token.NewFileSet()
	f, err := parser.ParseFile(apiFset, "exams.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("gagal parse exams.go: %v", err)
	}
	return f
}

// apiFuncDecl mencari deklarasi func berdasarkan nama.
func apiFuncDecl(t *testing.T, f *ast.File, name string) *ast.FuncDecl {
	t.Helper()
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if ok && fn.Name.Name == name {
			return fn
		}
	}
	t.Fatalf("fungsi %s tidak ditemukan di exams.go", name)
	return nil
}

// apiCalls mengembalikan nama fungsi yang dipanggil di dalam fn.
func apiCalls(t *testing.T, fn *ast.FuncDecl) map[string]int {
	t.Helper()
	out := map[string]int{}
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch id := call.Fun.(type) {
		case *ast.Ident:
			out[id.Name]++
		case *ast.SelectorExpr:
			out[id.Sel.Name]++
		}
		return true
	})
	return out
}

func sortedAPICalls(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestSubmitExamDerivesExpectedFieldsViaFailingClosedHelper mengunci
// SubmitExam memakai helper yang gagal tertutup.
func TestSubmitExamDerivesExpectedFieldsViaFailingClosedHelper(t *testing.T) {
	f := parseExamsSource(t)
	fn := apiFuncDecl(t, f, "SubmitExam")
	calls := apiCalls(t, fn)

	n, ok := calls["expectedIdentityFields"]
	if !ok {
		t.Fatalf("SubmitExam tidak memanggil expectedIdentityFields — "+
			"kolom identity_fields dibaca tanpa penanganan error, jadi config "+
			"rusak mematikan validasi identitas. Panggilan yang ada: %v",
			sortedAPICalls(calls))
	}
	if n != 1 {
		t.Errorf("SubmitExam memanggil expectedIdentityFields %d kali, "+
			"harusnya tepat 1", n)
	}
}

// TestSubmitExamStillValidatesRequiredIdentity ini mengunci rantai
// lengkap: field expectations → validasi → 400. Tanpa salah satu, config
// rusak atau field kosong lolos.
func TestSubmitExamStillValidatesRequiredIdentity(t *testing.T) {
	f := parseExamsSource(t)
	fn := apiFuncDecl(t, f, "SubmitExam")
	calls := apiCalls(t, fn)

	order := []string{
		"expectedIdentityFields",       // parse gagal tertutup
		"firstMissingRequiredIdentity", // cek field wajib terisi
		"errorResponse",                // 400 kalau ada yang kurang
	}
	for _, want := range order {
		if calls[want] == 0 {
			t.Errorf("SubmitExam tidak memanggil %s — rantai validasi "+
				"identitas pada jalur submit terputus", want)
		}
	}
	if calls["firstMissingRequiredIdentity"] != 1 {
		t.Errorf("SubmitExam memanggil firstMissingRequiredIdentity %d kali, "+
			"harusnya tepat 1", calls["firstMissingRequiredIdentity"])
	}
}

// TestNoSwallowedUnmarshalOfIdentityFieldsColumn menolak pola `_ = json.Unmarshal`
// terhadap kolom identity_fields di mana pun pada exams.go — itulah bug M8
// aslinya. Unmarshal yang_err-nya diperiksa sah untuk parse lain.
func TestNoSwallowedUnmarshalOfIdentityFieldsColumn(t *testing.T) {
	apiFset = token.NewFileSet()
	f, err := parser.ParseFile(apiFset, "exams.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("gagal parse exams.go: %v", err)
	}

	// Cari assignment `_ = json.Unmarshal(...)` dan lihat argumennyadirectly.
	ast.Inspect(f, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			return true
		}
		blank, ok := assign.Lhs[0].(*ast.Ident)
		if !ok || blank.Name != "_" {
			return true
		}
		call, ok := assign.Rhs[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "json" || sel.Sel.Name != "Unmarshal" {
			return true
		}
		line := apiFset.Position(assign.Pos()).Line
		// Unmarshal yang literal konstanta (defaultIdentityFields di init)
		// memang_recv error diabaikan secara wajar; yang berbahaya adalah
		// unmarshal pada DATA dari database.
		if len(call.Args) > 0 {
			if isByteSliceLiteral(call.Args[0]) {
				return true // literal konstanta, bukan data
			}
		}
		t.Errorf("exams.go:%d: `_ = json.Unmarshal(...)` pada data — "+
			"error parse ditelan diam-diam. Untuk kolom identity_fields itu "+
			"mematikan seluruh validasi identitas (M8). Periksa error-nya, "+
			"atau pakai expectedIdentityFields yang sudah gagal tertutup",
			line)
		return true
	})
}

// isByteSliceLiteral mengenali `[]byte("literal")`. `[]byte` di AST adalah
// *ast.ArrayType (bukan *ast.Ident), dan argumennya harus BasicLit string
// supaya konstanta `defaultIdentityFields` di init tidak salah flagged.
func isByteSliceLiteral(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	arr, ok := call.Fun.(*ast.ArrayType)
	if !ok || arr.Len != nil {
		return false
	}
	elt, ok := arr.Elt.(*ast.Ident)
	if !ok || (elt.Name != "byte" && elt.Name != "uint8") {
		return false
	}
	if len(call.Args) != 1 {
		return false
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	return ok && lit.Kind == token.STRING && len(lit.Value) > 2
}
