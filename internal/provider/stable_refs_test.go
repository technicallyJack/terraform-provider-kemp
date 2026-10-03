package provider

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/technicallyJack/terraform-provider-kemp/internal/client"
)

// testAccRenumber makes the LoadMaster renumber every virtual service. Any
// global configuration change does it; a throwaway cipher set is the least
// intrusive one.
func testAccRenumber(t *testing.T, c *client.Client) {
	t.Helper()
	ctx := context.Background()
	if err := c.Do(ctx, "modifycipherset", map[string]any{"name": "tfaccrenumber", "value": "ECDHE-RSA-AES128-GCM-SHA256"}, nil); err != nil {
		t.Fatalf("creating cipher set: %v", err)
	}
	if err := c.Do(ctx, "delcipherset", map[string]any{"name": "tfaccrenumber"}, nil); err != nil {
		t.Fatalf("deleting cipher set: %v", err)
	}
}

// findVS looks up a virtual service by nickname and address, never by index.
func findVS(t *testing.T, c *client.Client, nickname, addr string) *client.VirtualService {
	t.Helper()
	vss, err := c.ListVirtualServices(context.Background())
	if err != nil {
		t.Fatalf("listing virtual services: %v", err)
	}
	var found *client.VirtualService
	for i := range vss {
		if vss[i].NickName == nickname && (addr == "" || vss[i].VSAddress == addr) {
			if found != nil {
				t.Fatalf("more than one virtual service named %q", nickname)
			}
			found = &vss[i]
		}
	}
	return found
}

// TestAccStableReferences reproduces the LoadMaster renumbering virtual
// services behind Terraform's back: an out-of-band virtual service is added
// and a global change renumbers everything, moving the SubVS's index. The
// plan must stay empty and later changes must land on the right objects.
func TestAccStableReferences(t *testing.T) {
	addr := os.Getenv("KEMP_TEST_VS_ADDRESS")
	if addr == "" {
		t.Skip("KEMP_TEST_VS_ADDRESS not set")
	}
	rsAddr := os.Getenv("KEMP_TEST_RS_ADDRESS")
	if rsAddr == "" {
		rsAddr = "10.0.254.250"
	}
	c, err := sweeperClient()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if vs := findVS(t, c, "tf-acc-renum-oob", addr); vs != nil {
			_ = c.DeleteVirtualService(context.Background(), vs.Index)
		}
		// Renumber once more with the test objects gone, so the appliance's
		// other virtual services end up back at the indexes they had before.
		// Configurations still on index-based provider versions depend on it.
		testAccRenumber(t, c)
	})

	config := func(subWeight int, otherNick string) string {
		return fmt.Sprintf(`
resource "kemp_virtual_service" "parent" {
  address  = %[1]q
  port     = "18080"
  nickname = "tf-acc-renum-parent"
  type     = "http"
}

resource "kemp_sub_virtual_service" "sub" {
  parent_id = kemp_virtual_service.parent.id
  nickname  = "tf-acc-renum-sub"
  weight    = %[2]d
}

resource "kemp_real_server" "sub" {
  virtual_service_id = kemp_sub_virtual_service.sub.id
  address            = %[3]q
  port               = 18080
}

# Created after the SubVS, so it starts with a higher index and moves ahead
# of the SubVS when the LoadMaster renumbers.
resource "kemp_virtual_service" "other" {
  address    = %[1]q
  port       = "18082"
  nickname   = %[4]q
  depends_on = [kemp_sub_virtual_service.sub]
}
`, addr, subWeight, rsAddr, otherNick)
	}

	var indexesBefore map[string]int
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config(1000, "tf-acc-renum-other"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kemp_virtual_service.parent", "id", "tcp/"+addr+"/18080"),
					resource.TestMatchResourceAttr("kemp_sub_virtual_service.sub", "id", regexp.MustCompile(`^tcp/`+regexp.QuoteMeta(addr)+`/18080/sub/\d+$`)),
					resource.TestCheckResourceAttrPair("kemp_sub_virtual_service.sub", "parent_id", "kemp_virtual_service.parent", "id"),
					resource.TestMatchResourceAttr("kemp_real_server.sub", "id", regexp.MustCompile(`/sub/\d+/rs/\d+$`)),
					func(*terraform.State) error {
						indexesBefore = testObjectIndexes(t, c)
						return nil
					},
				),
			},
			{
				// Add a virtual service outside Terraform and force a renumber.
				PreConfig: func() {
					if _, err := c.CreateVirtualService(context.Background(), client.VirtualServiceParams{
						Address: &addr, Port: ptr("18083"), Protocol: ptr("tcp"), NickName: ptr("tf-acc-renum-oob"),
					}); err != nil {
						t.Fatalf("creating out-of-band virtual service: %v", err)
					}
					testAccRenumber(t, c)
					// Which objects move depends on what else is on the appliance,
					// but at least one of the test's own must have, or the step
					// below proves nothing.
					after := testObjectIndexes(t, c)
					if fmt.Sprint(after) == fmt.Sprint(indexesBefore) {
						t.Fatalf("renumbering didn't move any test object: %v", after)
					}
				},
				Config:   config(1000, "tf-acc-renum-other"),
				PlanOnly: true, // must be an empty plan
			},
			{
				// Changes after the renumber must reach the intended objects.
				Config: config(500, "tf-acc-renum-other2"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kemp_sub_virtual_service.sub", "weight", "500"),
					resource.TestCheckResourceAttr("kemp_virtual_service.other", "nickname", "tf-acc-renum-other2"),
					func(*terraform.State) error {
						if vs := findVS(t, c, "tf-acc-renum-oob", addr); vs == nil || vs.VSPort != "18083" {
							return fmt.Errorf("out-of-band virtual service was changed or removed: %+v", vs)
						}
						if vs := findVS(t, c, "tf-acc-renum-other2", addr); vs == nil || vs.VSPort != "18082" {
							return fmt.Errorf("rename didn't reach the right virtual service: %+v", vs)
						}
						return nil
					},
				),
			},
		},
	})
}

// TestAccUpgradeFromIndexState creates resources with v0.1.0 from the
// registry, which kept LoadMaster indexes in state, then switches to this
// build: the state upgraders and first refresh must convert everything to
// stable references with no planned changes.
func TestAccUpgradeFromIndexState(t *testing.T) {
	addr := os.Getenv("KEMP_TEST_VS_ADDRESS")
	if addr == "" {
		t.Skip("KEMP_TEST_VS_ADDRESS not set")
	}
	rsAddr := os.Getenv("KEMP_TEST_RS_ADDRESS")
	if rsAddr == "" {
		rsAddr = "10.0.254.250"
	}

	common := fmt.Sprintf(`
resource "kemp_virtual_service" "parent" {
  address  = %[1]q
  port     = "18080"
  nickname = "tf-acc-upgrade-parent"
  type     = "http"
}

resource "kemp_virtual_service" "plain" {
  address  = %[1]q
  port     = "18081"
  nickname = "tf-acc-upgrade-plain"

  # v0.1.0 creates virtual services concurrently, which the LoadMaster can
  # get wrong (TestAccParallelCreates); one at a time keeps this test about
  # the state upgrade.
  depends_on = [kemp_virtual_service.parent]
}
`, addr)
	oldConfig := common + fmt.Sprintf(`
resource "kemp_sub_virtual_service" "sub" {
  parent_index = kemp_virtual_service.parent.index
  nickname     = "tf-acc-upgrade-sub"
}

resource "kemp_real_server" "sub" {
  virtual_service_index = kemp_sub_virtual_service.sub.index
  address               = %[1]q
  port                  = 18080
}

resource "kemp_real_server" "plain" {
  virtual_service_index = kemp_virtual_service.plain.index
  address               = %[1]q
  port                  = 18080
}
`, rsAddr)
	newConfig := common + fmt.Sprintf(`
resource "kemp_sub_virtual_service" "sub" {
  parent_id = kemp_virtual_service.parent.id
  nickname  = "tf-acc-upgrade-sub"
}

resource "kemp_real_server" "sub" {
  virtual_service_id = kemp_sub_virtual_service.sub.id
  address            = %[1]q
  port               = 18080
}

resource "kemp_real_server" "plain" {
  virtual_service_id = kemp_virtual_service.plain.id
  address            = %[1]q
  port               = 18080
}
`, rsAddr)

	before := map[string]string{}
	resource.Test(t, resource.TestCase{
		PreCheck: func() { testAccPreCheck(t) },
		Steps: []resource.TestStep{
			{
				ExternalProviders: map[string]resource.ExternalProvider{
					"kemp": {Source: "technicallyjack/kemp", VersionConstraint: "0.1.0"},
				},
				Config: oldConfig,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("kemp_virtual_service.parent", "id", regexp.MustCompile(`^\d+$`)),
					remember(before),
				),
			},
			{
				// Upgrading must keep every object: the apply (which refreshes
				// first, as terraform plan/apply do) may not replace anything,
				// and the plan after it must be empty.
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Config:                   newConfig,
				Check: resource.ComposeAggregateTestCheckFunc(
					sameObjects(before),
					resource.TestCheckResourceAttr("kemp_virtual_service.parent", "id", "tcp/"+addr+"/18080"),
					resource.TestMatchResourceAttr("kemp_sub_virtual_service.sub", "id", regexp.MustCompile(`^tcp/.+/18080/sub/\d+$`)),
					resource.TestCheckResourceAttrPair("kemp_sub_virtual_service.sub", "parent_id", "kemp_virtual_service.parent", "id"),
					resource.TestCheckResourceAttrPair("kemp_real_server.sub", "virtual_service_id", "kemp_sub_virtual_service.sub", "id"),
					resource.TestCheckResourceAttrPair("kemp_real_server.plain", "virtual_service_id", "kemp_virtual_service.plain", "id"),
					resource.TestMatchResourceAttr("kemp_real_server.plain", "id", regexp.MustCompile(`^tcp/.+/18081/rs/\d+$`)),
				),
			},
		},
	})
}

func ptr(s string) *string { return &s }

// testObjectIndexes returns the current indexes of TestAccStableReferences'
// virtual services, by nickname.
func testObjectIndexes(t *testing.T, c *client.Client) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, n := range []string{"tf-acc-renum-parent", "tf-acc-renum-sub", "tf-acc-renum-other"} {
		if vs := findVS(t, c, n, ""); vs != nil {
			out[n] = vs.Index
		}
	}
	return out
}

// LoadMaster objects that must survive the upgrade: real server indexes never
// change, and the SubVS is recognisable by its nickname and its real server.
var upgradeTracked = []string{"kemp_real_server.sub", "kemp_real_server.plain"}

func remember(m map[string]string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		for _, name := range upgradeTracked {
			m[name] = s.RootModule().Resources[name].Primary.Attributes["index"]
		}
		return nil
	}
}

func sameObjects(m map[string]string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		for _, name := range upgradeTracked {
			if got := s.RootModule().Resources[name].Primary.Attributes["index"]; got != m[name] {
				return fmt.Errorf("%s was replaced during the upgrade: real server index %s, was %s", name, got, m[name])
			}
		}
		return nil
	}
}

// TestAccParallelCreates creates several virtual services in one apply with
// no dependencies between them, so Terraform creates them concurrently.
// Concurrent addvs calls on the LoadMaster can lose a virtual service while
// reporting success; the client serializes them and checks each one exists.
func TestAccParallelCreates(t *testing.T) {
	addr := os.Getenv("KEMP_TEST_VS_ADDRESS")
	if addr == "" {
		t.Skip("KEMP_TEST_VS_ADDRESS not set")
	}
	c, err := sweeperClient()
	if err != nil {
		t.Fatal(err)
	}
	const n = 6
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "kemp_virtual_service" "test" {
  count    = %d
  address  = %q
  port     = tostring(18300 + count.index)
  nickname = "tf-acc-parallel-${count.index}"
}
`, n, addr),
				Check: func(s *terraform.State) error {
					for i := 0; i < n; i++ {
						port := fmt.Sprint(18300 + i)
						vs := findVS(t, c, fmt.Sprintf("tf-acc-parallel-%d", i), addr)
						if vs == nil || vs.VSPort != port {
							return fmt.Errorf("virtual service %d (port %s) missing or wrong on the LoadMaster: %+v", i, port, vs)
						}
						got := s.RootModule().Resources[fmt.Sprintf("kemp_virtual_service.test.%d", i)].Primary.Attributes["id"]
						if want := "tcp/" + addr + "/" + port; got != want {
							return fmt.Errorf("virtual service %d has id %s, want %s", i, got, want)
						}
					}
					return nil
				},
			},
		},
	})
}

// rsList returns a virtual service's real servers with their status.
func rsList(t *testing.T, c *client.Client, vsIndex int) []client.RealServer {
	t.Helper()
	var out struct {
		Rs []client.RealServer `json:"Rs"`
	}
	if err := c.Do(t.Context(), "showvs", map[string]any{"vs": fmt.Sprint(vsIndex)}, &out); err != nil {
		t.Fatalf("showvs: %v", err)
	}
	return out.Rs
}
