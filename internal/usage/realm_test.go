package usage

import (
	"testing"
	"time"
)

func TestLedgerRealmFilterAppliesToEveryAggregate(t *testing.T) {
	r := New("")
	now := time.Now()
	r.Add(now, "cn", "cn-account", "shared-model", Delta{TotalTokens: 100, HasTotal: true, Credit: 1, HasCredit: true}, true)
	r.Add(now, "global", "global-account", "shared-model", Delta{TotalTokens: 900, HasTotal: true, Credit: 90, HasCredit: true}, true)
	for _, c := range []struct {
		realm   string
		tokens  int64
		credits float64
	}{
		{"cn", 100, 1}, {"global", 900, 90},
	} {
		s := r.SnapshotWindow(Window{Realm: c.realm}, nil, nil)
		if s.Totals.Requests != 1 || s.Totals.TotalTokens != c.tokens || s.Totals.Credits != c.credits {
			t.Fatalf("%s totals=%+v", c.realm, s.Totals)
		}
		if len(s.ByAccount) != 1 || len(s.ByRealm) != 1 || len(s.ByModel) != 1 || s.ByModel[0].TotalTokens != c.tokens || len(s.Series) != 1 || s.Series[0].Credits != c.credits {
			t.Fatalf("mixed realm leaked into %s: %+v", c.realm, s)
		}
		if len(s.CreditByModel) != 1 || s.CreditByModel[0].Realm != c.realm {
			t.Fatalf("cost model mixed realms: %+v", s.CreditByModel)
		}
	}
	if s := r.SnapshotWindow(Window{}, nil, nil); s.Totals.Requests != 2 {
		t.Fatal("legacy unfiltered API changed")
	}
}
