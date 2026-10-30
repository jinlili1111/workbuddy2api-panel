package panel

import (
	"net/http"
	"runtime"
	"time"
)

func (p *Panel) opsMonitor(w http.ResponseWriter, r *http.Request) {
	if p.cfg.RequestLog == nil {
		writeErr(w, http.StatusNotImplemented, "request logger not available")
		return
	}
	q, now := r.URL.Query(), time.Now()
	span := time.Hour
	if v := q.Get("window"); v != "" && v != "custom" {
		d, err := time.ParseDuration(v)
		if err != nil || d < time.Minute || d > 24*time.Hour {
			writeErr(w, 400, "时间窗口须为 1 分钟至 24 小时")
			return
		}
		span = d
	}
	from, to := now.Add(-span), now
	if q.Get("window") == "custom" {
		from, to = parseTimeParam(q.Get("from")), parseTimeParam(q.Get("to"))
		if from.IsZero() || to.IsZero() || !to.After(from) || to.Sub(from) > 24*time.Hour || to.After(now.Add(time.Minute)) || from.Before(now.Add(-24*time.Hour)) {
			writeErr(w, 400, "请选择最近 24 小时内的有效起止时间")
			return
		}
	}
	data := p.cfg.RequestLog.Monitor(from, to, q.Get("model"), q.Get("account"))
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	var accounts any = []any{}
	var locks any = []any{}
	total, healthy, cooling, disabled, paused, full := 0, 0, 0, 0, 0, 0
	if p.cfg.Pool != nil {
		total, healthy, cooling, disabled, paused, full = p.cfg.Pool.CountsDetailedWithPaused()
		accounts = p.cfg.Pool.List()
		locks = p.cfg.Pool.ModelLockView()
	}
	// Request details and system logs now have one canonical view each.
	data.Recent, data.RecentErrors, data.Models = nil, nil, nil
	writeJSON(w, 200, map[string]any{
		"collected_at": now, "data": data,
		"system":  map[string]any{"uptime_sec": int(time.Since(p.started).Seconds()), "goroutines": runtime.NumGoroutine(), "heap_bytes": mem.HeapAlloc, "runtime_bytes": mem.Sys, "gc_cycles": mem.NumGC, "redis_mode": p.cfg.RedisMode},
		"pool":    map[string]any{"total": total, "healthy": healthy, "cooling": cooling, "disabled": disabled, "paused": paused, "full": full, "accounts": accounts},
		"archive": p.cfg.RequestLog.Snapshot().Archive,
		"jobs":    p.ListTaskJobs(), "model_locks": locks,
	})
}
