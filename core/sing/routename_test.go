package sing

import (
	"context"
	"testing"

	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	R "github.com/sagernet/sing-box/route/rule"
	"github.com/sagernet/sing/common/json"

	"github.com/tavut846/FNode/api/panel"
)

func TestRouteNamesResolveFromBuiltRules(t *testing.T) {
	ctx := context.Background()
	nodes := []nodeRouting{{tag: "n1", routes: []panel.Route{
		{Id: 7, Remarks: "Netflix via US", Match: "netflix.com", Action: "route", ActionValue: "us"},
		{Id: 8, Match: "example.org", Action: "direct"}, // no name from an older panel
	}}}
	var rules []option.Rule
	var names []string
	for _, r := range compileScopedRules(false, nil, nodes) {
		r, name := takeRuleName(r)
		if _, leaked := r[ruleNameKey]; leaked {
			t.Fatal("name key must be stripped")
		}
		data, _ := json.Marshal(r)
		rule, err := json.UnmarshalExtendedContext[option.Rule](ctx, data)
		if err != nil {
			t.Fatal(err)
		}
		rules = append(rules, rule)
		names = append(names, name)
	}
	h := &HookServer{}
	h.SetRouteNames(ctx, log.NewNOPFactory().NewLogger("t"), rules, names)
	want := map[string]string{"netflix.com": "Netflix via US", "example.org": "route#8"}
	found := 0
	for i, opt := range rules {
		built, err := R.NewRule(ctx, log.NewNOPFactory().NewLogger("t"), opt, false)
		if err != nil {
			t.Fatal(err)
		}
		got := h.routeName(built)
		for dom, n := range want {
			if names[i] == n && got != n {
				t.Errorf("%s: got %q want %q", dom, got, n)
			}
			if names[i] == n {
				found++
			}
		}
	}
	if found != 2 {
		t.Errorf("found %d named rules, want 2", found)
	}
	if h.routeName(nil) != "default" {
		t.Error("no rule must log route=default")
	}
}
