package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccVirtualServicesDataSource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `data "kemp_virtual_services" "all" {}`,
				Check:  resource.TestCheckResourceAttrSet("data.kemp_virtual_services.all", "virtual_services.#"),
			},
		},
	})
}
