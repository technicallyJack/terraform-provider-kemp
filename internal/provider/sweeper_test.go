package provider

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/technicallyJack/terraform-provider-kemp/internal/client"
)

func TestMain(m *testing.M) {
	resource.TestMain(m)
}

// The sweepers remove objects left behind by failed acceptance
// tests: virtual services on KEMP_TEST_VS_ADDRESS whose nickname starts with
// "tf-acc" (deleting a parent also removes its SubVSs and real servers), and
// rules named tfacc_*.
// Run with `make sweep`.
func sweeperClient() (*client.Client, error) {
	return client.New(client.Config{
		Host:     os.Getenv("KEMP_HOST"),
		APIKey:   os.Getenv("KEMP_API_KEY"),
		Username: os.Getenv("KEMP_USERNAME"),
		Password: os.Getenv("KEMP_PASSWORD"),
		Insecure: os.Getenv("KEMP_INSECURE") == "true",
	})
}

func init() {
	// Rules named tfacc_* (rule names can't contain hyphens).
	resource.AddTestSweepers("kemp_rule", &resource.Sweeper{
		Name: "kemp_rule",
		F: func(_ string) error {
			c, err := sweeperClient()
			if err != nil {
				return err
			}
			ctx := context.Background()
			rules, err := c.ListRules(ctx)
			if err != nil {
				return err
			}
			for _, r := range rules {
				if !strings.HasPrefix(r.Name, "tfacc_") {
					continue
				}
				fmt.Printf("sweeping rule %s\n", r.Name)
				if err := c.DeleteRule(ctx, r.Name); err != nil && !client.IsNotFound(err) {
					return err
				}
			}
			return nil
		},
	})

	// Certificates and intermediates named tfacc_*, and the test cipher sets.
	resource.AddTestSweepers("kemp_certificate", &resource.Sweeper{
		Name:         "kemp_certificate",
		Dependencies: []string{"kemp_virtual_service"}, // in use certificates can't be deleted
		F: func(_ string) error {
			c, err := sweeperClient()
			if err != nil {
				return err
			}
			ctx := context.Background()
			certs, err := c.ListCertificates(ctx)
			if err != nil {
				return err
			}
			for _, cert := range certs {
				if strings.HasPrefix(cert.Name, "tfacc_") {
					fmt.Printf("sweeping certificate %s\n", cert.Name)
					_ = c.DeleteCertificate(ctx, cert.Name)
				}
			}
			ints, err := c.ListIntermediates(ctx)
			if err != nil {
				return err
			}
			for _, cert := range ints {
				if strings.HasPrefix(cert.Name, "tfacc_") {
					fmt.Printf("sweeping intermediate %s\n", cert.Name)
					_ = c.DeleteIntermediate(ctx, cert.Name)
				}
			}
			for _, cs := range []string{"tfacc_cs", "tfacc_cs_ssl"} { // no listing command exists
				if err := c.DeleteCipherSet(ctx, cs); err == nil {
					fmt.Printf("sweeping cipher set %s\n", cs)
				}
			}
			return nil
		},
	})

	resource.AddTestSweepers("kemp_virtual_service", &resource.Sweeper{
		Name: "kemp_virtual_service",
		F: func(_ string) error {
			addr := os.Getenv("KEMP_TEST_VS_ADDRESS")
			if addr == "" {
				return fmt.Errorf("KEMP_TEST_VS_ADDRESS must be set to sweep")
			}
			c, err := sweeperClient()
			if err != nil {
				return err
			}

			ctx := context.Background()
			vss, err := c.ListVirtualServices(ctx)
			if err != nil {
				return err
			}
			for _, vs := range vss {
				if vs.VSAddress != addr || !strings.HasPrefix(vs.NickName, "tf-acc") {
					continue
				}
				fmt.Printf("sweeping virtual service %d (%s)\n", vs.Index, vs.NickName)
				if err := c.DeleteVirtualService(ctx, vs.Index); err != nil && !client.IsNotFound(err) {
					return err
				}
			}
			return nil
		},
	})
}
