package reqlog

import (
	"sync"
	"testing"
	"time"
)

func TestMonitorWindowFiltersAndStreamFailures(t *testing.T) {
	r := New(Config{})
	defer r.Close()
	now := time.Now()
	r.Record(Event{Time: now.Add(-2 * time.Hour), Model: "m", OK: true})
	r.Record(Event{Time: now.Add(-time.Minute), Model: "m", Account: "a", Status: 200, OK: true, DurationMs: 1000, TTFBMs: 200, Attempts: 1, PromptTokens: 20, CompletionTokens: 80, TotalTokens: 100})
	r.Record(Event{Time: now.Add(-time.Second), Model: "m", Account: "a", Status: 200, OK: false, Outcome: OutcomeStreamError, DurationMs: 3000, Attempts: 3})
	r.Record(Event{Time: now, Model: "other", Account: "b", Status: 429, DurationMs: 5000, Outcome: OutcomeHTTPError})
	r.Begin()
	s := r.Monitor(now.Add(-time.Hour), now, "m", "a")
	if s.Totals.Requests != 2 || s.Totals.Failed != 1 || s.Totals.Succeeded != 1 || s.InFlight != 1 {
		t.Fatalf("totals: %+v", s)
	}
	if s.Totals.Retries != 2 || s.Totals.AvgDurationMs != 2000 || s.Totals.P50 != 1000 || s.Totals.P95 != 3000 {
		t.Fatalf("latency/retry: %+v", s.Totals)
	}
	if s.Totals.OutputTPS != 100 || s.Totals.TTFBSamples != 1 || s.Totals.TotalTokens != 100 {
		t.Fatalf("tokens: %+v", s.Totals)
	}
	if s.Errors[OutcomeStreamError] != 1 || len(s.Recent) != 2 || len(s.Models) != 1 || len(s.ModelOptions) != 2 {
		t.Fatalf("filtered result: %+v", s)
	}
	var requests, tokens, retries, errs int64
	for _, p := range s.Points {
		requests += p.Requests
		tokens += p.Tokens
		retries += p.Retries
		errs += p.Errors
	}
	if requests != 2 || tokens != 100 || retries != 2 || errs != 1 {
		t.Fatalf("chart totals mismatch: %d %d %d %d", requests, tokens, retries, errs)
	}
	all := r.Monitor(now.Add(-time.Hour), now, "", "")
	if all.Totals.Limited != 1 || all.Errors["limited"] != 1 {
		t.Fatalf("limited: %+v", all)
	}
	empty := r.Monitor(now.Add(-time.Hour), now, "absent", "")
	if empty.Totals.Requests != 0 || len(empty.Recent) != 0 || len(empty.Models) != 0 {
		t.Fatalf("empty: %+v", empty)
	}
}

func TestMonitorCapacityAndConcurrentRecording(t *testing.T) {
	r := New(Config{})
	defer r.Close()
	now := time.Now()
	// Fill through the observer directly to avoid exercising the separate recent log.
	r.mu.Lock()
	for i := 0; i < MonitorLimit+3; i++ {
		r.recordMonitor(Event{Time: now, OK: true})
	}
	r.mu.Unlock()
	s := r.Monitor(now.Add(-time.Minute), now, "", "")
	if s.Totals.Requests != MonitorLimit || !s.Truncated {
		t.Fatalf("capacity: %d truncated=%v", s.Totals.Requests, s.Truncated)
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 30; j++ {
				r.Begin()
				r.Record(Event{OK: true, Status: 200})
			}
		}()
	}
	for i := 0; i < 4; i++ {
		_ = r.Monitor(now.Add(-time.Hour), time.Now(), "", "")
	}
	wg.Wait()
	if r.Snapshot().InFlight != 0 {
		t.Fatal("request tracking leaked")
	}
}

func TestMonitorErrorDetailsSurviveRecentSuccessTraffic(t *testing.T) {
	r := New(Config{})
	defer r.Close()
	now := time.Now()
	r.Record(Event{Time: now.Add(-time.Minute), RequestID: "older-error", Status: 200, Outcome: OutcomeStreamError})
	for i := 0; i < 150; i++ {
		r.Record(Event{Time: now, Status: 200, OK: true})
	}
	s := r.Monitor(now.Add(-time.Hour), now, "", "")
	if len(s.Recent) != 100 || len(s.RecentErrors) != 1 || s.RecentErrors[0].RequestID != "older-error" {
		t.Fatalf("recent errors lost behind successful traffic: recent=%d errors=%+v", len(s.Recent), s.RecentErrors)
	}
}
