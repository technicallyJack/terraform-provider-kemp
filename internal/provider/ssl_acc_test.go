package provider

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func hclCert(name string, c *testCert, version int) string {
	return fmt.Sprintf(`
resource "kemp_certificate" %[1]q {
  name                   = "tfacc_%[1]s"
  certificate            = <<-EOT
%[2]s
EOT
  private_key_wo         = <<-EOT
%[3]s
EOT
  private_key_wo_version = %[4]d
}
`, name, c.CertPEM, c.KeyPEM, version)
}

func checkRemoteCert(name string, want *testCert) resource.TestCheckFunc {
	return func(*terraform.State) error {
		c, err := sweeperClient()
		if err != nil {
			return err
		}
		got, err := c.ReadCertificate(context.Background(), name)
		if err != nil {
			return err
		}
		if !sameCert(got, want.CertPEM) {
			return fmt.Errorf("LoadMaster has a different certificate for %s", name)
		}
		return nil
	}
}

func TestAccCertificateResource(t *testing.T) {
	first := genCert(t, "tfacc-cert.example.test", false, nil, true)
	second := genCert(t, "tfacc-cert2.example.test", false, nil, false)
	const name = "kemp_certificate.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      hclCert("test", &testCert{CertPEM: first.CertPEM + second.CertPEM, KeyPEM: first.KeyPEM}, 1),
				ExpectError: regexp.MustCompile(`kemp_intermediate_certificate`),
			},
			{
				Config: hclCert("test", first, 1),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "id", "tfacc_test"),
					resource.TestCheckResourceAttr(name, "subject", "CN=tfacc-cert.example.test"),
					resource.TestCheckResourceAttr(name, "dns_names.0", "tfacc-cert.example.test"),
					resource.TestCheckResourceAttr(name, "key_type", "RSA"),
					resource.TestCheckResourceAttrSet(name, "not_after"),
					resource.TestCheckNoResourceAttr(name, "private_key_wo"),
					checkRemoteCert("tfacc_test", first),
				),
			},
			{
				// A new certificate under the same name replaces it in place.
				Config: hclCert("test", second, 1),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(name, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "key_type", "ECC"),
					checkRemoteCert("tfacc_test", second),
				),
			},
			{
				// Bumping the version re-sends the key with an unchanged certificate.
				Config: hclCert("test", second, 2),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(name, plancheck.ResourceActionUpdate),
				}},
			},
			{
				ResourceName:      name,
				ImportState:       true,
				ImportStateId:     "tfacc_test",
				ImportStateVerify: true,
				// The certificate comes back in the LoadMaster's PEM formatting.
				ImportStateVerifyIgnore: []string{"private_key_wo_version", "certificate"},
			},
		},
	})
}

func TestAccIntermediateAndCipherSet(t *testing.T) {
	ca1 := genCert(t, "tfacc-ca1", true, nil, true)
	ca2 := genCert(t, "tfacc-ca2", true, nil, true)
	config := func(ca *testCert, ciphers string) string {
		return fmt.Sprintf(`
resource "kemp_intermediate_certificate" "test" {
  name        = "tfacc_int"
  certificate = <<-EOT
%s
EOT
}

resource "kemp_cipher_set" "test" {
  name    = "tfacc_cs"
  ciphers = [%s]
}
`, ca.CertPEM, ciphers)
	}
	const two = `"ECDHE-RSA-AES256-GCM-SHA384", "ECDHE-RSA-AES128-GCM-SHA256"`

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config(ca1, two),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kemp_intermediate_certificate.test", "subject", "CN=tfacc-ca1"),
					resource.TestCheckResourceAttr("kemp_cipher_set.test", "ciphers.#", "2"),
					resource.TestCheckResourceAttr("kemp_cipher_set.test", "ciphers.1", "ECDHE-RSA-AES128-GCM-SHA256"),
				),
			},
			{ResourceName: "kemp_intermediate_certificate.test", ImportState: true, ImportStateId: "tfacc_int", ImportStateVerify: true, ImportStateVerifyIgnore: []string{"certificate"}},
			{ResourceName: "kemp_cipher_set.test", ImportState: true, ImportStateId: "tfacc_cs", ImportStateVerify: true},
			{
				// Intermediates can't be replaced in place; cipher sets can.
				Config: config(ca2, `"ECDHE-RSA-AES128-GCM-SHA256"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("kemp_intermediate_certificate.test", plancheck.ResourceActionReplace),
					plancheck.ExpectResourceAction("kemp_cipher_set.test", plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kemp_intermediate_certificate.test", "subject", "CN=tfacc-ca2"),
					resource.TestCheckResourceAttr("kemp_cipher_set.test", "ciphers.#", "1"),
				),
			},
			{
				Config:      config(ca2, `"NOT-A-CIPHER"`),
				ExpectError: regexp.MustCompile(`Invalid Cipher list`),
			},
		},
	})
}

// waitListening waits for the LoadMaster to accept TCP connections on addr;
// it takes a few seconds after a virtual service changes.
func waitListening(addr string) error {
	var err error
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(time.Second) {
		var conn net.Conn
		if conn, err = net.DialTimeout("tcp", addr, 2*time.Second); err == nil {
			conn.Close()
			return nil
		}
	}
	return fmt.Errorf("%s not accepting connections: %w", addr, err)
}

// handshake connects to addr with TLS and returns the server certificate's
// subject, or the error.
func handshake(addr string, cfg *tls.Config) (string, error) {
	cfg.InsecureSkipVerify = true
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", addr, cfg)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	// With client certificates, TLS 1.3 servers may reject only after the
	// handshake, so read once to surface that.
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Read(make([]byte, 1)); err != nil {
		if ne, ok := err.(net.Error); !ok || !ne.Timeout() {
			return "", err
		}
	}
	return conn.ConnectionState().PeerCertificates[0].Subject.CommonName, nil
}

func TestAccVirtualServiceSSL(t *testing.T) {
	addr := os.Getenv("KEMP_TEST_VS_ADDRESS")
	if addr == "" {
		t.Skip("KEMP_TEST_VS_ADDRESS not set")
	}
	rsAddr := os.Getenv("KEMP_TEST_RS_ADDRESS")
	if rsAddr == "" {
		rsAddr = "10.0.254.250"
	}
	vip := net.JoinHostPort(addr, "18443")

	certA := genCert(t, "tfacc-a.example.test", false, nil, true)
	certB := genCert(t, "tfacc-b.example.test", false, nil, false)
	ca := genCert(t, "tfacc-client-ca", true, nil, true)
	client := genCert(t, "tfacc-client", false, ca, true)
	clientPair, err := tls.X509KeyPair([]byte(client.CertPEM), []byte(client.KeyPEM))
	if err != nil {
		t.Fatal(err)
	}

	common := hclCert("a", certA, 1) + hclCert("b", certB, 1) + fmt.Sprintf(`
resource "kemp_intermediate_certificate" "ca" {
  name        = "tfacc_client_ca"
  certificate = <<-EOT
%[1]s
EOT
}

resource "kemp_cipher_set" "cs" {
  name    = "tfacc_cs_ssl"
  ciphers = ["ECDHE-RSA-AES128-GCM-SHA256", "ECDHE-ECDSA-AES128-GCM-SHA256"]
}

resource "kemp_real_server" "test" {
  virtual_service_id = kemp_virtual_service.test.id
  address            = %[2]q
  port               = 18080
}
`, ca.CertPEM, rsAddr)
	vs := func(ssl string) string {
		return common + fmt.Sprintf(`
resource "kemp_virtual_service" "test" {
  address    = %q
  port       = "18443"
  nickname   = "tf-acc-ssl"
  type       = "http"
  check_type = "none"
  %s
}
`, addr, ssl)
	}
	const name = "kemp_virtual_service.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: vs(`ssl = {
    certificates = [kemp_certificate.a.name, kemp_certificate.b.name]
    tls_versions = ["1.2", "1.3"]
    cipher_set   = kemp_cipher_set.cs.name
  }`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "ssl.certificates.#", "2"),
					resource.TestCheckResourceAttr(name, "ssl.certificates.0", "tfacc_a"),
					resource.TestCheckResourceAttr(name, "ssl.tls_versions.#", "2"),
					resource.TestCheckResourceAttr(name, "ssl.cipher_set", "tfacc_cs_ssl"),
					resource.TestCheckResourceAttr(name, "ssl.client_certificate", "none"),
					func(*terraform.State) error {
						if err := waitListening(vip); err != nil {
							return err
						}
						// SNI picks the matching certificate.
						for _, want := range []string{"tfacc-a.example.test", "tfacc-b.example.test"} {
							got, err := handshake(vip, &tls.Config{ServerName: want, MinVersion: tls.VersionTLS12})
							if err != nil || got != want {
								return fmt.Errorf("SNI %s: got certificate %q, %v", want, got, err)
							}
						}
						// TLS 1.1 is off.
						if _, err := handshake(vip, &tls.Config{MinVersion: tls.VersionTLS11, MaxVersion: tls.VersionTLS11}); err == nil {
							return fmt.Errorf("TLS 1.1 handshake succeeded with tls_versions 1.2/1.3")
						}
						return nil
					},
				),
			},
			{ResourceName: name, ImportState: true, ImportStateId: "tcp/" + addr + "/18443", ImportStateVerify: true},
			{
				Config: vs(`ssl = {
    certificates       = [kemp_certificate.a.name]
    reencrypt          = true
    pass_sni           = true
    http2              = true
    client_certificate = "required"
  }`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "ssl.reencrypt", "true"),
					resource.TestCheckResourceAttr(name, "ssl.pass_sni", "true"),
					resource.TestCheckResourceAttr(name, "ssl.http2", "true"),
					resource.TestCheckResourceAttr(name, "ssl.tls_versions.#", "3"),
					resource.TestCheckResourceAttr(name, "ssl.cipher_set", "Default"),
					func(*terraform.State) error {
						if err := waitListening(vip); err != nil {
							return err
						}
						time.Sleep(2 * time.Second) // let the new SSL settings take effect
						if _, err := handshake(vip, &tls.Config{MaxVersion: tls.VersionTLS12}); err == nil {
							return fmt.Errorf("handshake without a client certificate succeeded with client_certificate = required")
						}
						if _, err := handshake(vip, &tls.Config{MaxVersion: tls.VersionTLS12, Certificates: []tls.Certificate{clientPair}}); err != nil {
							return fmt.Errorf("handshake with a trusted client certificate failed: %v", err)
						}
						return nil
					},
				),
			},
			{
				Config:      vs(`ssl = { certificates = [kemp_certificate.a.name], pass_sni = true }`),
				ExpectError: regexp.MustCompile(`pass_sni requires reencrypt`),
			},
			{
				// Removing the block turns SSL off (and resets the options).
				Config: vs(""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr(name, "ssl.certificates.#"),
					func(*terraform.State) error {
						c, err := sweeperClient()
						if err != nil {
							return err
						}
						v := findVS(t, c, "tf-acc-ssl", addr)
						if v == nil || v.SSLAcceleration || v.SSLReencrypt || v.ClientCert != 0 {
							return fmt.Errorf("SSL not fully off: %+v", v)
						}
						return nil
					},
				),
			},
		},
	})
}
