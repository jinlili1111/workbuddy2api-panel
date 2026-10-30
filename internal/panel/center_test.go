package panel

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/misakano7545/workbuddy2api-panel/internal/reqlog"
)

func TestRequestTraceExplicitMemoryFallbackHonorsRange(t *testing.T) {
	r := reqlog.New(reqlog.Config{})
	defer r.Close()
	r.Record(reqlog.Event{Time: time.Now(), RequestID: "current", Status: 200, OK: false, Outcome: reqlog.OutcomeStreamError})
	p := New(Config{RequestLog: r})
	for _, c := range []struct {
		query, source string
		count         int
	}{
		{"?fallback=memory", "memory", 1},
		{"?fallback=memory&from=" + time.Now().UTC().Add(time.Hour).Format(time.RFC3339), "memory", 0},
		{"?fallback=memory&model=missing", "memory", 0},
		{"", "archive", 0},
	} {
		w := httptest.NewRecorder()
		p.ServeHTTP(w, httptest.NewRequest("GET", "/panel/api/request_logs"+c.query, nil))
		var body struct {
			Source  string         `json:"source"`
			Entries []reqlog.Event `json:"entries"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if w.Code != 200 || body.Source != c.source || len(body.Entries) != c.count || body.Entries == nil {
			t.Fatalf("%s: %s", c.query, w.Body.String())
		}
	}
}

func TestCenterRoutesAndRefreshStayReadOnly(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	script := `const fs=require('fs'),vm=require('vm'),assert=require('assert');
const source=fs.readFileSync('app.js','utf8');
const elements={};const el=id=>elements[id]||(elements[id]={id,hidden:false,textContent:'',classList:{toggle(){}},setAttribute(){}});
const sections=['ops','usage','events','logs','accounts'].map(v=>({id:'view-'+v}));
let route='',calls=[];
const ctx={$:el,view:'accounts',navSet(){},document:{querySelectorAll:q=>q==='.view'?sections:[]},history:{replaceState:(a,b,url)=>route=url},setTimeout(){},opsSetFullscreen(){},logRows:[],resetLogWindow(){},console,Promise};
for(const name of ['loadOps','loadUsage','loadCreditHistory','loadEvents','loadLogs'])ctx[name]=async()=>calls.push(name);
vm.createContext(ctx);
vm.runInContext(source.slice(source.indexOf('const TITLES ='),source.indexOf('/* ── 账号池')),ctx);
vm.runInContext("go('usage')",ctx);
assert.strictEqual(route,'#ops/usage');assert.strictEqual(el('centerNav').hidden,false);
assert.strictEqual(el('ttl').textContent,'运行中心');assert.strictEqual(sections.find(s=>s.id==='view-usage').hidden,false);
assert.ok(calls.includes('loadCreditHistory'));assert.ok(calls.includes('loadUsage'));
vm.runInContext("go('ops/requests')",ctx);assert.strictEqual(ctx.view,'events');assert.strictEqual(route,'#ops/requests');
vm.runInContext("go('accounts')",ctx);assert.strictEqual(el('centerNav').hidden,true);
ctx.api=async()=>{throw new Error('observability refresh must not call write API')};
ctx.loadOverview=async()=>{throw new Error('must refresh selected center tab')};
ctx.toast=()=>{};
const first=source.indexOf("$('btnRefresh').onclick = async () =>"),last=source.indexOf('/* ── 轮询',first);
vm.runInContext(source.slice(first,last),ctx);
(async()=>{for(const v of ['ops','usage','events','logs']){ctx.view=v;calls=[];await el('btnRefresh').onclick();assert.ok(calls.length>0);}})().catch(e=>{console.error(e);process.exitCode=1});
`
	path := filepath.Join(t.TempDir(), "center.cjs")
	if err := os.WriteFile(path, []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, path).CombinedOutput(); err != nil {
		t.Fatalf("center: %v\n%s", err, out)
	}
}

func TestLedgerRealmSwitchRejectsStaleResults(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	script := `const fs=require('fs'),vm=require('vm'),assert=require('assert');
const source=fs.readFileSync('app.js','utf8'),requests=[];
const realm={value:'cn'},ctx={usageGeneration:0,URLSearchParams,$:()=>realm,trangeQuery:()=>new URLSearchParams(),warmUsageModelRates:async()=>{},api:q=>new Promise((resolve,reject)=>requests.push({q,resolve,reject})),renderUsage:d=>ctx.shown=d};
vm.createContext(ctx);
vm.runInContext(source.slice(source.indexOf('async function loadUsage()'),source.indexOf("if ($('btnUsage'))")),ctx);
(async()=>{
 const cn=ctx.loadUsage();await new Promise(setImmediate);realm.value='global';const global=ctx.loadUsage();await new Promise(setImmediate);
 assert.ok(requests[0].q.includes('realm=cn'));assert.ok(requests[1].q.includes('realm=global'));
 requests[1].resolve({realm:'global'});await global;requests[0].resolve({realm:'cn'});await cn;
 assert.strictEqual(ctx.shown.realm,'global','late CN response must not overwrite selected global ledger');
 realm.value='cn';const old=ctx.loadUsage();await new Promise(setImmediate);realm.value='global';const latest=ctx.loadUsage();await new Promise(setImmediate);
 requests[3].resolve({realm:'global'});await latest;requests[2].reject(new Error('late error'));await old;
 assert.strictEqual(ctx.shown.realm,'global','late error must not clear current data');
})().catch(e=>{console.error(e);process.exitCode=1});`
	path := filepath.Join(t.TempDir(), "ledger-race.cjs")
	if err := os.WriteFile(path, []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, path).CombinedOutput(); err != nil {
		t.Fatalf("ledger realm: %v\n%s", err, out)
	}
}
