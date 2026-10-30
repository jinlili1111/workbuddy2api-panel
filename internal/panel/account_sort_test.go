package panel

import (
	"os"
	"os/exec"
	"testing"
)

func TestAccountTableSortKeepsSnapshotAndRefreshOrder(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	script := `const fs = require('fs'), vm = require('vm'), assert = require('assert');
const source = fs.readFileSync('app.js', 'utf8');
const first = source.indexOf("let accRealmFilter ="), last = source.indexOf('// renderModelLocks');
const elements = {};
for (const id of ['accBody','accSortStatus','accSortCredits','accSortStatusHeading','accSortCreditsHeading']) {
  elements[id] = {innerHTML:'', textContent:'', attrs:{}, setAttribute(k,v){this.attrs[k]=v;}};
}
const ctx = {$: id => elements[id] || null, setHTML:(el,html)=>el.innerHTML=html,
  esc: String, dur:()=>'', ago:()=>'', remarkName:(id,html)=>html,
  rateLimitRowsHtml:()=>'', formatTokenCount:()=>'',formatLatency:()=>'',formatRate:()=>'',
  disabledKind:()=>null, disabledAge:()=>'', Date, Number, Math};
vm.createContext(ctx);
vm.runInContext(source.slice(first,last) + '\nthis.render=renderAccounts;this.toggle=toggleAccountSort;this.realm=r=>{accRealmFilter=r;renderAccounts(lastAccountsList)};',ctx);
const rows = [
  {uid:'low',credits:2}, {uid:'high',credits:20,realm:'global'},
  {uid:'unknown',credits:null}, {uid:'unlimited',credits_total:-1,enterprise:true},
  {uid:'equal',credits:20}, {uid:'off',credits:5,disabled:true},
  {uid:'paused',credits:4,paused:true}, {uid:'cool',credits:3,cool_remaining_sec:90},
];
const original = JSON.stringify(rows);
const ids = () => [...elements.accBody.innerHTML.matchAll(/<tr class="[^"]*" title="uid: ([^"]+)">/g)].map(x=>x[1]);
ctx.render(rows);
assert.deepStrictEqual(ids(),rows.map(x=>x.uid));
ctx.toggle('credits');
assert.deepStrictEqual(ids(),['unlimited','high','equal','off','paused','cool','low','unknown']);
assert.strictEqual(elements.accSortCreditsHeading.attrs['aria-sort'],'descending');
ctx.toggle('credits');
assert.deepStrictEqual(ids(),['low','cool','paused','off','high','equal','unlimited','unknown']);
ctx.render(rows.map(s=>({...s,credits:s.uid==='low'?30:s.credits})));
assert.strictEqual(ids().indexOf('low') > ids().indexOf('high'),true);
ctx.realm('cn'); assert.strictEqual(ids().includes('high'),false);
ctx.realm('all'); ctx.toggle('status');
assert.deepStrictEqual(ids().slice(-3),['cool','paused','off']);
ctx.toggle('status'); assert.deepStrictEqual(ids().slice(0,3),['off','paused','cool']);
assert.strictEqual(elements.accSortStatusHeading.attrs['aria-sort'],'descending');
assert.strictEqual(elements.accSortCreditsHeading.attrs['aria-sort'],'none');
assert.strictEqual(JSON.stringify(rows),original,'sorting must not mutate the shared snapshot');
ctx.render([]); assert.ok(elements.accBody.innerHTML.includes('账号池是空的'));
const many=Array.from({length:41},(_,i)=>({uid:'page-'+i,credits:i,realm:i%2?'global':'cn'}));
vm.runInContext("accRealmFilter='all';accSort={key:'',direction:1};accPage=1;",ctx);
ctx.render(many); assert.strictEqual(ids().length,10);assert.strictEqual(ids()[0],'page-0');
vm.runInContext('accPage=3',ctx);ctx.render(many);assert.strictEqual(ids()[0],'page-20');
ctx.render(many.map(x=>({...x,credits:x.credits+1})));assert.strictEqual(ids()[0],'page-20','refresh keeps current page');
vm.runInContext('accPage=5',ctx);ctx.render(many);assert.deepStrictEqual(ids(),['page-40']);
ctx.render(many.slice(0,20));assert.strictEqual(ids()[0],'page-10','shrinking list clamps last page');
ctx.toggle('credits');assert.strictEqual(ids()[0],'page-19','sort resets page and sorts entire pool');
vm.runInContext("accRealmFilter='global';accPage=1;",ctx);ctx.render(many);
assert.strictEqual(ids().length,10);assert.strictEqual(ids()[0],'page-39','realm filtering happens before pagination');
`
	f, err := os.CreateTemp(t.TempDir(), "account-sort-*.cjs")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(script); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if output, err := exec.Command(node, f.Name()).CombinedOutput(); err != nil {
		t.Fatalf("account sorting: %v\n%s", err, output)
	}
}
