package panel

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/misakano7545/workbuddy2api-panel/internal/reqlog"
)

func TestOpsAuthenticationAndWindowValidation(t *testing.T) {
	r := reqlog.New(reqlog.Config{})
	defer r.Close()
	r.Record(reqlog.Event{Status: 200, OK: false, Outcome: reqlog.OutcomeStreamError, Model: "test"})
	p := New(Config{APIKey: "ops-test", RequestLog: r})
	for _, c := range []struct {
		query, key string
		status     int
	}{
		{"", "", 401}, {"", "wrong", 401}, {"", "ops-test", 200},
		{"?window=0s", "ops-test", 400}, {"?window=48h", "ops-test", 400}, {"?window=bad", "ops-test", 400},
		{"?window=custom&from=bad&to=bad", "ops-test", 400}, {"?window=5m&model=missing", "ops-test", 200},
	} {
		req := httptest.NewRequest("GET", "/panel/api/ops"+c.query, nil)
		if c.key != "" {
			req.Header.Set("Authorization", "Bearer "+c.key)
		}
		w := httptest.NewRecorder()
		p.ServeHTTP(w, req)
		if w.Code != c.status {
			t.Fatalf("%s key=%s status=%d body=%s", c.query, c.key, w.Code, w.Body.String())
		}
		if c.status == 200 {
			var body struct {
				Data reqlog.MonitorSnapshot `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if c.query == "" && body.Data.Totals.Failed != 1 {
				t.Fatalf("HTTP 200 stream failure lost: %+v", body)
			}
			if c.query != "" && body.Data.Totals.Requests != 0 {
				t.Fatal("model filter ignored")
			}
		}
	}
}
