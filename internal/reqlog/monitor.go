package reqlog

import (
	"sort"
	"time"
)

// MonitorLimit bounds observation memory even when the panel is never opened.
// This window deliberately does not read archives on each browser refresh.
const MonitorLimit = 50000

func (r *Recorder) recordMonitor(e Event) {
	// The monitor never needs caller IP or user agent.
	e.ClientIP, e.UserAgent = "", ""
	if len(r.monitor) < MonitorLimit {
		r.monitor = append(r.monitor, e)
		return
	}
	if old := r.monitor[r.monitorNext].Time; old.After(r.evictedAt) {
		r.evictedAt = old
	}
	r.monitor[r.monitorNext] = e
	r.monitorNext = (r.monitorNext + 1) % MonitorLimit
}

type MonitorTotals struct {
	Requests      int64   `json:"requests"`
	Succeeded     int64   `json:"succeeded"`
	Failed        int64   `json:"failed"`
	Limited       int64   `json:"limited"`
	Retries       int64   `json:"retries"`
	PromptTokens  int64   `json:"prompt_tokens"`
	OutputTokens  int64   `json:"output_tokens"`
	TotalTokens   int64   `json:"total_tokens"`
	TTFBSamples   int64   `json:"ttfb_samples"`
	TPSamples     int64   `json:"tps_samples"`
	AvgDurationMs float64 `json:"avg_duration_ms"`
	AvgTTFBMs     float64 `json:"avg_ttfb_ms"`
	OutputTPS     float64 `json:"output_tps"`
	P50           float64 `json:"p50"`
	P90           float64 `json:"p90"`
	P95           float64 `json:"p95"`
	P99           float64 `json:"p99"`
}
type MonitorPoint struct {
	Time     time.Time `json:"time"`
	Requests int64     `json:"requests"`
	Errors   int64     `json:"errors"`
	Tokens   int64     `json:"tokens"`
	Retries  int64     `json:"retries"`
}
type MonitorModel struct {
	Model string `json:"model"`
	MonitorTotals
}
type MonitorSnapshot struct {
	From           time.Time        `json:"from"`
	To             time.Time        `json:"to"`
	StartedAt      time.Time        `json:"started_at"`
	Truncated      bool             `json:"truncated"`
	Limit          int              `json:"limit"`
	InFlight       int64            `json:"in_flight"` // whole gateway; not affected by history filters
	Totals         MonitorTotals    `json:"totals"`
	Points         []MonitorPoint   `json:"points"`
	StepSec        float64          `json:"step_sec"`
	Histogram      []int64          `json:"histogram"`
	Errors         map[string]int64 `json:"errors"`
	Models         []MonitorModel   `json:"models"`
	ModelOptions   []string         `json:"model_options"`
	AccountOptions []string         `json:"account_options"`
	Recent         []Event          `json:"recent"`
	RecentErrors   []Event          `json:"recent_errors"`
}
type monitorAggregate struct {
	MonitorTotals
	durations []int64
	ttfbSum   int64
	tpsSum    float64
}

func (a *monitorAggregate) add(e Event) {
	a.Requests++
	if e.OK {
		a.Succeeded++
	} else {
		a.Failed++
	}
	if e.Status == 429 || e.Status == 529 {
		a.Limited++
	}
	if e.Attempts > 1 {
		a.Retries += int64(e.Attempts - 1)
	}
	a.PromptTokens += e.PromptTokens
	a.OutputTokens += e.CompletionTokens
	a.TotalTokens += e.TotalTokens
	a.durations = append(a.durations, e.DurationMs)
	a.AvgDurationMs += float64(e.DurationMs)
	if e.TTFBMs > 0 {
		a.ttfb(e.TTFBMs)
	}
	if e.OK && e.CompletionTokens > 0 && e.TTFBMs > 0 && e.DurationMs > e.TTFBMs {
		a.TPSamples++
		a.tpsSum += float64(e.CompletionTokens) * 1000 / float64(e.DurationMs-e.TTFBMs)
	}
}
func (a *monitorAggregate) ttfb(n int64) { a.TTFBSamples++; a.ttfbSum += n }
func (a *monitorAggregate) finish() MonitorTotals {
	if a.Requests == 0 {
		return a.MonitorTotals
	}
	a.AvgDurationMs /= float64(a.Requests)
	if a.TTFBSamples > 0 {
		a.AvgTTFBMs = float64(a.ttfbSum) / float64(a.TTFBSamples)
	}
	if a.TPSamples > 0 {
		a.OutputTPS = a.tpsSum / float64(a.TPSamples)
	}
	sort.Slice(a.durations, func(i, j int) bool { return a.durations[i] < a.durations[j] })
	quantile := func(p int) float64 { return float64(a.durations[(len(a.durations)*p+99)/100-1]) }
	a.P50, a.P90, a.P95, a.P99 = quantile(50), quantile(90), quantile(95), quantile(99)
	return a.MonitorTotals
}

// Monitor uses completed request time, exact model/account filters, and a bounded
// 24h in-memory window. All totals and charts use the same set of events.
func (r *Recorder) Monitor(from, to time.Time, model, account string) MonitorSnapshot {
	r.mu.Lock()
	events := append([]Event(nil), r.monitor...)
	s := MonitorSnapshot{From: from, To: to, StartedAt: r.started, Truncated: from.Before(r.started) || !r.evictedAt.Before(from), Limit: MonitorLimit, InFlight: r.inFlight}
	r.mu.Unlock()
	s.Points = make([]MonitorPoint, 60)
	s.StepSec = to.Sub(from).Seconds() / 60
	s.Histogram = make([]int64, 7)
	s.Errors = map[string]int64{}
	s.Models, s.Recent, s.RecentErrors = []MonitorModel{}, []Event{}, []Event{}
	s.ModelOptions, s.AccountOptions = []string{}, []string{}
	for i := range s.Points {
		s.Points[i].Time = from.Add(time.Duration(float64(i) * s.StepSec * float64(time.Second)))
	}
	all := monitorAggregate{}
	models := map[string]*monitorAggregate{}
	mo, ao := map[string]bool{}, map[string]bool{}
	for _, e := range events {
		if e.Time.Before(from) || e.Time.After(to) {
			continue
		}
		if e.Model != "" {
			mo[e.Model] = true
		}
		if e.Account != "" {
			ao[e.Account] = true
		}
		if model != "" && e.Model != model || account != "" && e.Account != account {
			continue
		}
		all.add(e)
		if models[e.Model] == nil {
			models[e.Model] = &monitorAggregate{}
		}
		models[e.Model].add(e)
		i := int(e.Time.Sub(from).Seconds() / s.StepSec)
		if i >= 60 {
			i = 59
		}
		if i < 0 {
			i = 0
		}
		pt := &s.Points[i]
		pt.Requests++
		pt.Tokens += e.TotalTokens
		if !e.OK {
			s.RecentErrors = append(s.RecentErrors, e)
			pt.Errors++
			kind := e.Outcome
			if kind == OutcomeHTTPError {
				if e.Status == 429 || e.Status == 529 {
					kind = "limited"
				} else if e.Status >= 500 {
					kind = "server"
				} else {
					kind = "client"
				}
			}
			s.Errors[kind]++
		}
		if e.Attempts > 1 {
			pt.Retries += int64(e.Attempts - 1)
		}
		bucket := 6
		for j, v := range []int64{1000, 3000, 5000, 10000, 30000, 60000} {
			if e.DurationMs <= v {
				bucket = j
				break
			}
		}
		s.Histogram[bucket]++
		s.Recent = append(s.Recent, e)
	}
	s.Totals = all.finish()
	for name, a := range models {
		s.Models = append(s.Models, MonitorModel{Model: name, MonitorTotals: a.finish()})
	}
	sort.Slice(s.Models, func(i, j int) bool {
		if s.Models[i].Requests == s.Models[j].Requests {
			return s.Models[i].Model < s.Models[j].Model
		}
		return s.Models[i].Requests > s.Models[j].Requests
	})
	for k := range mo {
		s.ModelOptions = append(s.ModelOptions, k)
	}
	sort.Strings(s.ModelOptions)
	for k := range ao {
		s.AccountOptions = append(s.AccountOptions, k)
	}
	sort.Strings(s.AccountOptions)
	sort.Slice(s.Recent, func(i, j int) bool { return s.Recent[i].Time.After(s.Recent[j].Time) })
	sort.Slice(s.RecentErrors, func(i, j int) bool { return s.RecentErrors[i].Time.After(s.RecentErrors[j].Time) })
	if len(s.RecentErrors) > 100 {
		s.RecentErrors = s.RecentErrors[:100]
	}
	if len(s.Recent) > 100 {
		s.Recent = s.Recent[:100]
	}
	return s
}
