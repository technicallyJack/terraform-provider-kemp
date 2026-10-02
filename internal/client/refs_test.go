package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRefsRoundTrip(t *testing.T) {
	for _, s := range []string{"tcp/10.0.0.1/443", "udp/10.0.0.1/53", "tcp/10.0.0.1/443/sub/2", "tcp/fd00::1/443"} {
		r, err := ParseVSRef(s)
		if err != nil || r.String() != s {
			t.Errorf("ParseVSRef(%q) = %v, %v", s, r, err)
		}
	}
	for _, s := range []string{"tcp/10.0.0.1/443/rs/5", "tcp/10.0.0.1/443/sub/2/rs/6"} {
		r, err := ParseRSRef(s)
		if err != nil || r.String() != s {
			t.Errorf("ParseRSRef(%q) = %v, %v", s, r, err)
		}
	}
	for _, bad := range []string{"", "3", "tcp/10.0.0.1", "tcp/10.0.0.1/443/sub/x", "tcp/10.0.0.1/443/sub/0", "tcp//443", "tcp/10.0.0.1/443/foo/2"} {
		if _, err := ParseVSRef(bad); err == nil {
			t.Errorf("ParseVSRef(%q) should fail", bad)
		}
	}
	for _, bad := range []string{"tcp/10.0.0.1/443", "tcp/10.0.0.1/443/rs/x", "3/5"} {
		if _, err := ParseRSRef(bad); err == nil {
			t.Errorf("ParseRSRef(%q) should fail", bad)
		}
	}
}

func TestResolveVS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		switch {
		case b["cmd"] == "showvs" && b["vs"] == "10.0.0.1" && b["port"] == "443" && b["prot"] == "tcp":
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","Index":7,"VSAddress":"10.0.0.1","VSPort":"443","Protocol":"tcp","SubVS":[{"VSIndex":9,"RsIndex":2}]}`))
		case b["cmd"] == "showvs" && b["vs"] == "9":
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","Index":9,"MasterVSID":7,"NickName":"sub"}`))
		case b["cmd"] == "showvs" && b["vs"] == "7":
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","Index":7,"VSAddress":"10.0.0.1","VSPort":"443","Protocol":"tcp","SubVS":[{"VSIndex":9,"RsIndex":2}]}`))
		default:
			_, _ = w.Write([]byte(`{"code":422,"message":"Unknown VS","status":"fail"}`))
		}
	}))
	defer srv.Close()
	c, _ := New(Config{Host: srv.URL, APIKey: "k", Insecure: true})
	ctx := context.Background()

	vs, err := c.ResolveVS(ctx, VSRef{Protocol: "tcp", Address: "10.0.0.1", Port: "443"})
	if err != nil || vs.Index != 7 {
		t.Fatalf("top-level: %+v %v", vs, err)
	}
	sub, err := c.ResolveVS(ctx, VSRef{Protocol: "tcp", Address: "10.0.0.1", Port: "443", SubSlot: 2})
	if err != nil || sub.Index != 9 {
		t.Fatalf("SubVS: %+v %v", sub, err)
	}
	if _, err := c.ResolveVS(ctx, VSRef{Protocol: "tcp", Address: "10.0.0.1", Port: "443", SubSlot: 3}); !IsNotFound(err) {
		t.Fatalf("missing slot: %v", err)
	}
	if _, err := c.ResolveVS(ctx, VSRef{Protocol: "udp", Address: "10.0.0.1", Port: "443"}); !IsNotFound(err) {
		t.Fatalf("missing VS: %v", err)
	}

	ref, err := c.RefForIndex(ctx, 9)
	if err != nil || ref.String() != "tcp/10.0.0.1/443/sub/2" {
		t.Fatalf("RefForIndex(9) = %v, %v", ref, err)
	}
	ref, err = c.RefForIndex(ctx, 7)
	if err != nil || ref.String() != "tcp/10.0.0.1/443" {
		t.Fatalf("RefForIndex(7) = %v, %v", ref, err)
	}

	if err := CheckSame(VSRef{Protocol: "tcp", Address: "10.0.0.1", Port: "443"}, &VirtualService{Protocol: "tcp", VSAddress: "10.0.0.2", VSPort: "443"}, 0); err == nil {
		t.Fatal("CheckSame should catch a different virtual service")
	}
}
