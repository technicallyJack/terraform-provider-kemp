package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListVirtualServices(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/accessv2" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["cmd"] != "listvs" || body["apikey"] != "secret" {
			t.Errorf("unexpected body %v", body)
		}
		_, _ = w.Write([]byte(`{"code":200,"message":"Command successfully executed","status":"ok",
			"VS":[{"Index":1,"NickName":"web","VSAddress":"10.0.0.10","VSPort":"443","Protocol":"tcp","Status":"Up","Enable":true}]}`))
	}))
	defer srv.Close()

	c, err := New(Config{Host: srv.URL, APIKey: "secret", Insecure: true})
	if err != nil {
		t.Fatal(err)
	}
	vss, err := c.ListVirtualServices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(vss) != 1 || vss[0].NickName != "web" || vss[0].Index != 1 {
		t.Fatalf("unexpected result: %+v", vss)
	}
}

func TestAPIError(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":401,"message":"Authorization Required","status":"fail"}`))
	}))
	defer srv.Close()

	c, _ := New(Config{Host: srv.URL, APIKey: "bad", Insecure: true})
	err := c.Do(context.Background(), "listvs", nil, nil)
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Code != 401 {
		t.Fatalf("expected 401 APIError, got %v", err)
	}
}

func TestHTMLErrorPages(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   string
	}{
		{http.StatusUnauthorized, "authentication failed"},
		{http.StatusNotFound, "API interface is enabled"},
	} {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(`<!DOCTYPE html><HTML><BODY>error</BODY></HTML>`))
		}))

		c, _ := New(Config{Host: srv.URL, APIKey: "k", Insecure: true})
		err := c.Do(context.Background(), "listvs", nil, nil)
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Code != tc.status || !strings.Contains(apiErr.Message, tc.want) {
			t.Errorf("HTTP %d: got %v", tc.status, err)
		}
		srv.Close()
	}
}

func TestIsNotFound(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":422,"message":"Unknown VS","status":"fail"}`))
	}))
	defer srv.Close()

	c, _ := New(Config{Host: srv.URL, APIKey: "k", Insecure: true})
	_, err := c.GetVirtualService(context.Background(), 999)
	if !IsNotFound(err) {
		t.Fatalf("expected not found, got %v", err)
	}
	if IsNotFound(&APIError{Code: 422, Message: "Invalid port"}) {
		t.Fatal("validation error should not count as not found")
	}
}

func TestVirtualServiceRequestParams(t *testing.T) {
	var got map[string]any
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = nil
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"code":200,"status":"ok","Index":7,"VSAddress":"10.0.0.5","VSPort":"80","Protocol":"tcp","NickName":"t","Enable":true,"VStype":"gen"}`))
	}))
	defer srv.Close()

	c, _ := New(Config{Host: srv.URL, APIKey: "k", Insecure: true})
	str := func(s string) *string { return &s }
	enable := true

	vs, err := c.CreateVirtualService(context.Background(), VirtualServiceParams{
		Address: str("10.0.0.5"), Port: str("80"), Protocol: str("tcp"), NickName: str("t"), Enable: &enable, VSType: str("gen"),
	})
	if err != nil || vs.Index != 7 {
		t.Fatalf("create: %v %+v", err, vs)
	}
	for k, v := range map[string]any{"cmd": "addvs", "vs": "10.0.0.5", "port": "80", "prot": "tcp", "NickName": "t", "Enable": true, "VStype": "gen"} {
		if got[k] != v {
			t.Errorf("addvs %s = %v, want %v", k, got[k], v)
		}
	}

	if _, err := c.UpdateVirtualService(context.Background(), 7, VirtualServiceParams{Address: str("10.0.0.6"), Port: str("81")}); err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]any{"cmd": "modvs", "vs": "7", "VSAddress": "10.0.0.6", "VSPort": "81"} {
		if got[k] != v {
			t.Errorf("modvs %s = %v, want %v", k, got[k], v)
		}
	}
}

func TestRealServerRequestParams(t *testing.T) {
	var got map[string]any
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = nil
		_ = json.NewDecoder(r.Body).Decode(&got)
		if got["cmd"] == "modrs" {
			_, _ = w.Write([]byte(`{"code":200,"message":"Command completed ok","status":"ok"}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":200,"status":"ok","Rs":[{"VSIndex":4,"RsIndex":8,"Addr":"10.0.254.250","Port":18080,"Forward":"nat","Weight":1000,"Limit":0,"Enable":true}]}`))
	}))
	defer srv.Close()

	c, _ := New(Config{Host: srv.URL, APIKey: "k", Insecure: true})

	rs, err := c.CreateRealServer(context.Background(), 4, "10.0.254.250", 18080)
	if err != nil || rs.RsIndex != 8 || rs.Port != 18080 {
		t.Fatalf("create: %v %+v", err, rs)
	}
	for k, v := range map[string]any{"cmd": "addrs", "vs": "4", "rs": "10.0.254.250", "rsport": "18080"} {
		if got[k] != v {
			t.Errorf("addrs %s = %v, want %v", k, got[k], v)
		}
	}

	port, weight, fwd := 18081, 200, "route"
	if err := c.UpdateRealServer(context.Background(), 4, 8, RealServerParams{Port: &port, Weight: &weight, Forward: &fwd}); err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]any{"cmd": "modrs", "vs": "4", "rs": "!8", "NewPort": "18081", "Weight": "200", "Forward": "route"} {
		if got[k] != v {
			t.Errorf("modrs %s = %v, want %v", k, got[k], v)
		}
	}

	if _, err := c.GetRealServer(context.Background(), 4, 8); err != nil || got["cmd"] != "showrs" || got["rs"] != "!8" {
		t.Fatalf("showrs: %v %v", err, got)
	}
}
