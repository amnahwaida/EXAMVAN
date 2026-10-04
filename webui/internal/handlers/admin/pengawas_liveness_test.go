package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/examvan/webui/internal/database"
)

// ---------------------------------------------------------------------------
// The supervision page polls its submissions endpoint forever, but the payload
// carries no indication of whether the exam is still running. Everything the
// supervisor does on that page (approve, reject, start, stop) is refused by the
// server once the exam ends — yet the UI keeps looking like a live control
// panel, and only reveals the truth through a toast after a failed tap.
// ---------------------------------------------------------------------------

// TestSubmissionsPayloadCarriesExamLiveness pins that the polled payload says
// whether the exam is still running and whether its window has passed, so the
// page can show that instead of waiting for a rejected action.
func TestSubmissionsPayloadCarriesExamLiveness(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)
	ctx := context.Background()

	get := func() map[string]interface{} {
		t.Helper()
		srv := httptest.NewServer(newAutoApproveTestRouter(pool))
		defer srv.Close()
		jar, _ := cookiejar.New(nil)
		client := &http.Client{Jar: jar}
		if resp, err := client.Post(srv.URL+"/test/login/"+strconv.Itoa(fx.PwID), "", nil); err != nil {
			t.Fatalf("login: %v", err)
		} else {
			resp.Body.Close()
		}
		req, _ := http.NewRequest(http.MethodGet,
			srv.URL+"/admin/api/pengawas/exams/"+strconv.Itoa(fx.ExamID)+"/submissions", nil)
		req.Header.Set("Accept", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("GET submissions: %v", err)
		}
		defer resp.Body.Close()
		var out map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return out
	}

	// 1) A running exam must report itself as live.
	live := get()
	if v, ok := live["exam_active"]; !ok {
		t.Errorf("payload has no exam_active flag — the polled page cannot tell a "+
			"running exam from a finished one, so controls stay enabled and every "+
			"action is refused server-side: keys=%v", keysOf(live))
	} else if v != true {
		t.Errorf("exam_active = %v, want true for a running exam", v)
	}
	if v, ok := live["exam_schedule_ended"]; !ok {
		t.Errorf("payload has no exam_schedule_ended flag: keys=%v", keysOf(live))
	} else if v != false {
		t.Errorf("exam_schedule_ended = %v, want false for an exam inside its window", v)
	}

	// 2) A STOPPED exam must be reported as not live.
	if _, err := pool.Exec(ctx,
		`UPDATE exams SET status = 'inactive' WHERE id = $1`, fx.ExamID); err != nil {
		t.Fatalf("stop exam: %v", err)
	}
	stopped := get()
	if stopped["exam_active"] != false {
		t.Errorf("exam_active = %v after stopping the exam, want false", stopped["exam_active"])
	}

	// 3) An exam whose window has passed must be reported as ended even while
	//    still flagged active — this is the case where start/stop and approval
	//    all start failing while the UI still shows a live control panel.
	if _, err := pool.Exec(ctx,
		`UPDATE exams SET status = 'active', end_time = $2 WHERE id = $1`,
		fx.ExamID, time.Now().Add(-time.Hour).Format(time.RFC3339)); err != nil {
		t.Fatalf("set past end_time: %v", err)
	}
	ended := get()
	if ended["exam_schedule_ended"] != true {
		t.Errorf("exam_schedule_ended = %v for an exam whose window passed, want true",
			ended["exam_schedule_ended"])
	}
}

func keysOf(m map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
