package provider

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// testAccProtoV6ProviderFactories is used by acceptance tests to instantiate
// the provider in-process.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"kemp": providerserver.NewProtocol6WithError(New("test")()),
}

func testAccPreCheck(t *testing.T) {
	t.Helper()
	if os.Getenv("KEMP_HOST") == "" {
		t.Fatal("KEMP_HOST must be set for acceptance tests")
	}
	if os.Getenv("KEMP_API_KEY") == "" && (os.Getenv("KEMP_USERNAME") == "" || os.Getenv("KEMP_PASSWORD") == "") {
		t.Fatal("KEMP_API_KEY, or KEMP_USERNAME and KEMP_PASSWORD, must be set for acceptance tests")
	}
}
