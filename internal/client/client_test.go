package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
