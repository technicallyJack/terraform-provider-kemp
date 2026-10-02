package provider

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// certNameValidator: certificate and cipher set names may contain hyphens and
// dots (unlike rule names), but not whitespace, since a virtual service lists
// its certificates space-separated.
var certNameValidator = stringvalidator.RegexMatches(regexp.MustCompile(`^[A-Za-z0-9_.-]+$`),
	"may only contain letters, digits, underscores, hyphens and dots")

// parseSingleCert parses PEM holding exactly one certificate. The LoadMaster
// stores only the first certificate of a chain, so a chain would never match
// what's read back.
func parseSingleCert(s string) (*x509.Certificate, error) {
	var certs []*x509.Certificate
	rest := []byte(s)
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			return nil, fmt.Errorf("found a %q PEM block; only the certificate belongs here", block.Type)
		}
		c, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parsing certificate: %w", err)
		}
		certs = append(certs, c)
	}
	switch len(certs) {
	case 0:
		return nil, fmt.Errorf("no PEM certificate found")
	case 1:
		return certs[0], nil
	default:
		return nil, fmt.Errorf("found %d certificates; give only the leaf and install the rest with kemp_intermediate_certificate", len(certs))
	}
}

// sameCert reports whether two PEM certificates are the same certificate,
// ignoring formatting.
func sameCert(a, b string) bool {
	ca, errA := parseSingleCert(a)
	cb, errB := parseSingleCert(b)
	return errA == nil && errB == nil && bytes.Equal(ca.Raw, cb.Raw)
}

func keyType(c *x509.Certificate) string {
	switch c.PublicKey.(type) {
	case *rsa.PublicKey:
		return "RSA"
	case *ecdsa.PublicKey:
		return "ECC"
	case ed25519.PublicKey:
		return "Ed25519"
	}
	return "unknown"
}

// certInfo holds the computed attributes derived from a certificate.
type certInfo struct {
	Subject  types.String
	DNSNames types.List
	NotAfter types.String
	KeyType  types.String
}

func certInfoFrom(pemStr string) certInfo {
	c, err := parseSingleCert(pemStr)
	if err != nil {
		return certInfo{
			Subject:  types.StringUnknown(),
			DNSNames: types.ListUnknown(types.StringType),
			NotAfter: types.StringUnknown(),
			KeyType:  types.StringUnknown(),
		}
	}
	names := make([]attr.Value, len(c.DNSNames))
	for i, n := range c.DNSNames {
		names[i] = types.StringValue(n)
	}
	return certInfo{
		Subject:  types.StringValue(c.Subject.String()),
		DNSNames: types.ListValueMust(types.StringType, names),
		NotAfter: types.StringValue(c.NotAfter.UTC().Format(time.RFC3339)),
		KeyType:  types.StringValue(keyType(c)),
	}
}

func normalizePEM(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n")) + "\n"
}
