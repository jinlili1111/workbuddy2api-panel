package panel

// 用量总览 KPI 的前端切片测试（仿 credit_frontend_adversarial_test.go 的 node + vm 做法）。
// 钉住「平均缓存命中率」卡：位置在「请求数」「总 token」之后、口径 = 命中/(命中+未命中)、
// 无样本时显示占位符；字符串脏值不得抛异常。只新增测试，不改业务代码。

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

const usOverviewScript = `const fs = require('fs');
const vm = require('vm');
const src = fs.readFileSync(process.argv[2], 'utf8');
const escStart = src.indexOf('function esc(');
const escEnd = src.indexOf('function ago(');
const cacheStart = src.indexOf('function cacheRateText');
const cacheEnd = src.indexOf('function fmtBytes');
const kpiStart = src.indexOf('function fmtTok(n)');
const kpiEnd = src.indexOf('/* renderUsageChart');
if ([escStart, escEnd, cacheStart, cacheEnd, kpiStart, kpiEnd].some(v => v < 0))
  throw new Error('usage overview helpers not found');
const els = {};
const $ = id => (els[id] || (els[id] =
  { innerHTML: '', textContent: '', title: '', dataset: {}, addEventListener() {}, onchange: null }));
const ctx = { Date, Number, String, Math, RegExp, isNaN, parseFloat, parseInt, JSON, $ };
vm.createContext(ctx);
// 切片外的依赖以桩代之：模块级状态、时区/备注/图表与两个缓存格式化函数。
vm.runInContext(
  'let usDim="account", usCreditDim="account", usSort="total", usageData=null;' +
  'function trangeLabel(){ return "近7天"; }' +
  'function remarkName(u, h){ return h; }' +
  'function renderUsageChart(){ }' +
  // renderUsage 末尾还会填按小时表，那个函数在切片之外（切片止于 renderUsageChart 注释）。
  'function renderUsageHours(){ }' +
  'function fmtHit(){ return "—"; }' +
  'function cacheRateCell(){ return "—"; }' +
  src.slice(escStart, escEnd) + src.slice(cacheStart, cacheEnd) + src.slice(kpiStart, kpiEnd) +
  '\nthis.renderUsage=renderUsage;', ctx);
const out = {};
const run = (key, fn) => { try { fn(); out[key] = true; } catch (e) { out[key] = String((e && e.stack) || e); } };

run('sampleOk', () => ctx.renderUsage({ totals: {
  requests: 10, errors: 1, prompt_tokens: 9000, completion_tokens: 1000, total_tokens: 10000,
  avg_latency_ms: 1200, avg_tokens_per_second: 42.5,
  credits: 1.23, credit_samples: 3,
  cache_hit_tokens: 7200, cache_miss_tokens: 800 }, buckets: 3 }));
out.sampleStats = els.usCreditStats.innerHTML;

run('emptyOk', () => ctx.renderUsage({ totals: {} }));
out.emptyStats = els.usCreditStats.innerHTML;

run('dirtyOk', () => ctx.renderUsage({ totals: { requests: '3', cache_hit_tokens: '7200', cache_miss_tokens: 'x' } }));
out.dirtyStats = els.usCreditStats.innerHTML;
console.log(JSON.stringify(out));
`

// TestUsageOverviewCacheHitKPI 验证总览第三张主指标卡「平均缓存命中率」的渲染。
func TestUsageOverviewCacheHitKPI(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; usage overview frontend test skipped")
	}
	f, err := os.CreateTemp(t.TempDir(), "us-kpi-*.cjs")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(usOverviewScript); err != nil {
		t.Fatal(err)
	}
	f.Close()
	raw, err := exec.Command(node, f.Name(), "app.js").CombinedOutput()
	if err != nil {
		t.Fatalf("usage overview node test failed: %v\n%s", err, raw)
	}
	var out struct {
		SampleOk    bool   `json:"sampleOk"`
		SampleStats string `json:"sampleStats"`
		EmptyOk     bool   `json:"emptyOk"`
		EmptyStats  string `json:"emptyStats"`
		DirtyOk     bool   `json:"dirtyOk"`
		DirtyStats  string `json:"dirtyStats"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("解析 node 输出失败: %v\n%s", err, raw)
	}
	for _, c := range []struct {
		ok  bool
		msg string
	}{
		{out.SampleOk, "正常样本渲染抛异常"},
		{out.EmptyOk, "空 totals 渲染抛异常"},
		{out.DirtyOk, "字符串脏值渲染抛异常"},
	} {
		if !c.ok {
			t.Fatalf("renderUsage %s", c.msg)
		}
	}

	stats := out.SampleStats
	for _, label := range []string{"扣除积分", "1.23", "90%", "命中 7.2k · 未命中 800", "有效积分样本"} {
		if !strings.Contains(stats, label) {
			t.Fatalf("积分账本缺少 %s: %s", label, stats)
		}
	}
	if strings.Contains(stats, ">请求数<") || strings.Contains(stats, ">平均延迟<") {
		t.Fatal("积分账本不应重复实时健康指标")
	}
	if !strings.Contains(out.EmptyStats, "无积分样本") || !strings.Contains(out.EmptyStats, "无缓存观测样本") || !strings.Contains(out.EmptyStats, ">—<") {
		t.Fatalf("无样本必须保留未知状态: %s", out.EmptyStats)
	}
	// 脏值：'7200'/'x' 应按 Number() 归零/取值，不产生 NaN%。
	if strings.Contains(out.DirtyStats, "NaN") {
		t.Fatalf("脏值渲染出 NaN:\n%s", out.DirtyStats)
	}
}

// The ledger keeps cost metrics and does not expose another eight-card request overview.
func TestUsageLedgerHasSingleCostSummary(t *testing.T) {
	raw, err := os.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(raw)
	if strings.Contains(html, `id="usStats"`) {
		t.Fatal("duplicate request KPIs remain in the ledger")
	}
	if strings.Count(html, `id="usCreditStats"`) != 1 {
		t.Fatal("cost summary must occur once")
	}
}
