package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestPlanRuleChanges(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		current, desired       []string
		wantDetach, wantAttach []string
	}{
		{"no change", []string{"a", "b"}, []string{"a", "b"}, nil, nil},
		{"append", []string{"a"}, []string{"a", "b", "c"}, nil, []string{"b", "c"}},
		{"remove middle", []string{"a", "b", "c"}, []string{"a", "c"}, []string{"b"}, nil},
		{"remove all", []string{"a", "b"}, nil, []string{"a", "b"}, nil},
		{"insert at front", []string{"a", "b"}, []string{"x", "a", "b"}, []string{"a", "b"}, []string{"x", "a", "b"}},
		{"swap last two", []string{"a", "b", "c"}, []string{"a", "c", "b"}, []string{"b", "c"}, []string{"c", "b"}},
		{"remove and append", []string{"a", "b"}, []string{"a", "c"}, []string{"b"}, []string{"c"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			detach, attach := planRuleChanges(tc.current, tc.desired)
			if !slices.Equal(detach, tc.wantDetach) || !slices.Equal(attach, tc.wantAttach) {
				t.Errorf("got detach=%v attach=%v, want detach=%v attach=%v", detach, attach, tc.wantDetach, tc.wantAttach)
			}
		})
	}
}

func TestRuleParamsAndGetRule(t *testing.T) {
	var got []map[string]any
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		got = append(got, body)
		if body["cmd"] == "showrule" {
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","AddHeaderRule":[{"Name":"other","Header":"X","HeaderValue":"1"}],
				"ReplaceHeaderRule":[{"Name":"rep","Pattern":"old","Header":"Host","Replacement":"new"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":200,"message":"Command completed ok","status":"ok"}`))
	}))
	defer srv.Close()
	c, _ := New(Config{Host: srv.URL, APIKey: "k", Insecure: true})
	ctx := context.Background()

	rule, err := c.GetRule(ctx, "rep")
	if err != nil || rule.Type != RuleTypeReplaceHeader || rule.Header != "Host" || rule.Replacement != "new" {
		t.Fatalf("GetRule: %+v %v", rule, err)
	}

	if err := c.CreateRule(ctx, "m", RuleParams{Type: RuleTypeMatch, Pattern: "/api/", MatchType: "prefix", CaseIndependent: true, SetFlagOnMatch: 2}); err != nil {
		t.Fatal(err)
	}
	m := got[len(got)-1]
	for k, v := range map[string]any{"cmd": "addrule", "name": "m", "type": "0", "pattern": "/api/", "matchtype": "prefix", "nocase": "1", "negate": "0", "setonmatch": "2", "onlyonflag": "0", "header": ""} {
		if m[k] != v {
			t.Errorf("addrule %s = %v, want %v", k, m[k], v)
		}
	}

	if err := c.UpdateRule(ctx, "h", RuleParams{Type: RuleTypeAddHeader, Header: "X-Env", Replacement: "prod"}); err != nil {
		t.Fatal(err)
	}
	m = got[len(got)-1]
	if m["cmd"] != "modrule" || m["type"] != "1" || m["header"] != "X-Env" || m["replacement"] != "prod" || m["pattern"] != nil {
		t.Errorf("modrule add header: %v", m)
	}

	got = nil
	if err := c.SetRules(ctx, RSMatchRules(4, 8), []string{"a", "b"}, []string{"b", "a"}); err != nil {
		t.Fatal(err)
	}
	var cmds []string
	for _, b := range got {
		if b["vs"] != "4" || b["rs"] != "!8" {
			t.Errorf("unexpected target %v", b)
		}
		cmds = append(cmds, b["cmd"].(string)+":"+b["rule"].(string))
	}
	if want := []string{"delrsrule:a", "delrsrule:b", "addrsrule:b", "addrsrule:a"}; !slices.Equal(cmds, want) {
		t.Errorf("SetRules calls = %v, want %v", cmds, want)
	}
}
