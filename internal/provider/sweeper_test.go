package provider

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"gitea.jacksonwinkler.com/jackson.winkler/terraform-provider-kemp/internal/client"
)

func TestMain(m *testing.M) {
	resource.TestMain(m)
}

// The sweeper removes virtual services left behind by failed acceptance
// tests: only those on KEMP_TEST_VS_ADDRESS whose nickname starts with
// "tf-acc". Deleting a parent also removes its SubVSs and real servers.
// Run with `make sweep`.
func init() {
	resource.AddTestSweepers("kemp_virtual_service", &resource.Sweeper{
		Name: "kemp_virtual_service",
		F: func(_ string) error {
			addr := os.Getenv("KEMP_TEST_VS_ADDRESS")
			if addr == "" {
				return fmt.Errorf("KEMP_TEST_VS_ADDRESS must be set to sweep")
			}
			c, err := client.New(client.Config{
				Host:     os.Getenv("KEMP_HOST"),
				APIKey:   os.Getenv("KEMP_API_KEY"),
				Username: os.Getenv("KEMP_USERNAME"),
				Password: os.Getenv("KEMP_PASSWORD"),
				Insecure: os.Getenv("KEMP_INSECURE") == "true",
			})
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
