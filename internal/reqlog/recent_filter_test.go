package reqlog

import (
	"testing"
	"time"
)

func TestRecentFallbackSortsBeforeFilteringAndLimiting(t *testing.T) {
	r := New(Config{})
	defer r.Close()
	now := time.Now()
	r.Record(Event{Time: now, RequestID: "newest", Model: "m"})
	r.Record(Event{Time: now.Add(-time.Minute), RequestID: "older", Model: "m"})
	r.Record(Event{Time: now.Add(time.Minute), RequestID: "other", Model: "other"})
	rows := r.ReadRecent(1, Filter{Model: "m"})
	if len(rows) != 1 || rows[0].RequestID != "newest" {
		t.Fatalf("newest matching request lost: %+v", rows)
	}
	if rows = r.ReadRecent(5, Filter{From: now.Add(2 * time.Minute)}); len(rows) != 0 {
		t.Fatalf("out-of-range records leaked: %+v", rows)
	}
}
