package provider

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// echoed is what the test real server saw.
type echoed struct {
	Client  string            `json:"client"`
	Headers map[string]string `json:"headers"`
}

// startEcho runs a real server on :18090 that echoes the client address and
// request headers it received, padded so responses are worth compressing.
func startEcho(t *testing.T) {
	t.Helper()
	srv := &http.Server{Addr: ":18090", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		h := map[string]string{}
		for k := range r.Header {
			h[k] = r.Header.Get(k)
		}
		b, _ := json.Marshal(echoed{Client: host, Headers: h})
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write(append(append(b, '\n'), []byte(strings.Repeat("padding ", 600))...))
	})}
	go func() { _ = srv.ListenAndServe() }()
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })
}

// fetchVIP requests the VIP through the LoadMaster, retrying while the
// virtual service settles. It returns what the real server saw and whether
// the response came back gzip-encoded.
func fetchVIP(vip string) (*echoed, bool, error) {
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{DisableCompression: true}}
	var lastErr error
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(2 * time.Second) {
		req, _ := http.NewRequest(http.MethodGet, "http://"+vip+"/", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		var body io.Reader = resp.Body
		gzipped := resp.Header.Get("Content-Encoding") == "gzip"
		if gzipped {
			if body, err = gzip.NewReader(resp.Body); err != nil {
				resp.Body.Close()
				return nil, false, err
			}
		}
		raw, err := io.ReadAll(body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		var e echoed
		if err := json.Unmarshal([]byte(strings.SplitN(string(raw), "\n", 2)[0]), &e); err != nil {
			lastErr = fmt.Errorf("unexpected response %q", string(raw[:min(len(raw), 80)]))
			continue
		}
		return &e, gzipped, nil
	}
	return nil, false, lastErr
}

func TestAccVirtualServiceProxySettings(t *testing.T) {
	addr := os.Getenv("KEMP_TEST_VS_ADDRESS")
	if addr == "" {
		t.Skip("KEMP_TEST_VS_ADDRESS not set")
	}
	// Optional: this machine's address as the LoadMaster sees it, for traffic checks.
	echoAddr := os.Getenv("KEMP_TEST_ECHO_ADDRESS")
	rsAddr, rsPort := echoAddr, 18090
	if echoAddr == "" {
		rsAddr, rsPort = "10.0.254.250", 18080
	} else {
		startEcho(t)
	}
	vip := net.JoinHostPort(addr, "18080")

	config := func(settings string) string {
		return fmt.Sprintf(`
resource "kemp_virtual_service" "test" {
  address    = %q
  port       = "18080"
  nickname   = "tf-acc-proxy"
  check_type = "none"
  %s
}

resource "kemp_real_server" "test" {
  virtual_service_id = kemp_virtual_service.test.id
  address            = %q
  port               = %d
}
`, addr, settings, rsAddr, rsPort)
	}
	// The LoadMaster reports new settings at once but takes a few seconds to
	// apply them to traffic, so keep checking until they show or time runs out.
	traffic := func(check func(*echoed, bool) error) resource.TestCheckFunc {
		return func(*terraform.State) error {
			if echoAddr == "" {
				return nil
			}
			var err error
			for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(2 * time.Second) {
				e, gz, fetchErr := fetchVIP(vip)
				if fetchErr != nil {
					err = fmt.Errorf("requesting %s: %w", vip, fetchErr)
					continue
				}
				if err = check(e, gz); err == nil {
					return nil
				}
			}
			return err
		}
	}
	const name = "kemp_virtual_service.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config(`type               = "http"
  forwarded_headers  = "x_forwarded_for"
  subnet_originating = false
  compress           = true
  idle_timeout       = 120`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "forwarded_headers", "x_forwarded_for"),
					resource.TestCheckResourceAttr(name, "subnet_originating", "false"),
					resource.TestCheckResourceAttr(name, "compress", "true"),
					resource.TestCheckResourceAttr(name, "idle_timeout", "120"),
					resource.TestCheckResourceAttrSet(name, "transparent"),
					traffic(func(e *echoed, gz bool) error {
						switch {
						case e.Headers["X-Forwarded-For"] == "":
							return fmt.Errorf("no X-Forwarded-For reached the real server: %v", e.Headers)
						case e.Headers["Via"] != "":
							return fmt.Errorf("unexpected Via header with x_forwarded_for: %v", e.Headers)
						case e.Client != addr:
							return fmt.Errorf("with subnet_originating = false the real server should see the VIP %s, saw %s", addr, e.Client)
						case !gz:
							return fmt.Errorf("response wasn't gzip-encoded with compress = true")
						}
						return nil
					}),
				),
			},
			{ResourceName: name, ImportState: true, ImportStateVerify: true},
			{
				// Type and headers change together: the type goes first, so
				// its reset of the header setting doesn't win.
				Config: config(`type              = "gen"
  forwarded_headers = "x_clientside_and_via"
  cache             = true
  idle_timeout      = 3600`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "type", "gen"),
					resource.TestCheckResourceAttr(name, "forwarded_headers", "x_clientside_and_via"),
					resource.TestCheckResourceAttr(name, "cache", "true"),
					// Left unset: keeps the value from the previous step.
					resource.TestCheckResourceAttr(name, "subnet_originating", "false"),
					resource.TestCheckResourceAttr(name, "compress", "false"),
				),
			},
			{
				Config: config(`type               = "http"
  forwarded_headers  = "x_forwarded_for_and_via"
  subnet_originating = true`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "forwarded_headers", "x_forwarded_for_and_via"),
					traffic(func(e *echoed, gz bool) error {
						switch {
						case e.Headers["X-Forwarded-For"] == "" || e.Headers["Via"] == "":
							return fmt.Errorf("expected X-Forwarded-For and Via, got %v", e.Headers)
						case e.Client == addr:
							return fmt.Errorf("with subnet_originating = true the real server shouldn't see the VIP")
						case gz:
							return fmt.Errorf("response gzip-encoded with compress = false")
						}
						return nil
					}),
				),
			},
			{
				// Back to the defaults.
				Config: config(""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "forwarded_headers", "legacy"),
					resource.TestCheckResourceAttr(name, "idle_timeout", "660"),
					resource.TestCheckResourceAttr(name, "cache", "false"),
					resource.TestCheckResourceAttr(name, "force_l7", "true"),
				),
			},
		},
	})
}
