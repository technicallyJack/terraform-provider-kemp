package provider

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"slices"
	"testing"
	"time"
)

// testCert is a generated certificate and key, in PEM.
type testCert struct {
	CertPEM, KeyPEM string
	cert            *x509.Certificate
	key             any
}

// genCert makes a certificate for cn (also used as its DNS name), signed by
// parent, or self-signed when parent is nil. ca makes it a CA.
func genCert(t *testing.T, cn string, ca bool, parent *testCert, rsaKey bool) *testCert {
	t.Helper()
	var key any
	var pub any
	if rsaKey {
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		key, pub = k, &k.PublicKey
	} else {
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		key, pub = k, &k.PublicKey
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: cn},
		DNSNames:              []string{cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(30 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  ca,
	}
	if ca {
		tmpl.KeyUsage |= x509.KeyUsageCertSign
	}
	signerCert, signerKey := tmpl, key
	if parent != nil {
		signerCert, signerKey = parent.cert, parent.key
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, signerCert, pub, signerKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := x509.ParseCertificate(der)
	return &testCert{
		CertPEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		KeyPEM:  string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})),
		cert:    c,
		key:     key,
	}
}

func TestTLSMask(t *testing.T) {
	for _, tc := range []struct {
		versions []string
		mask     int
	}{
		{[]string{"1.1", "1.2", "1.3"}, 3}, // the LoadMaster's default
		{[]string{"1.2", "1.3"}, 7},
		{[]string{"1.0", "1.1", "1.2", "1.3"}, 1},
		{[]string{"1.3"}, 15},
		{[]string{"1.2"}, 23},
	} {
		if got := tlsMask(tc.versions); got != tc.mask {
			t.Errorf("tlsMask(%v) = %d, want %d", tc.versions, got, tc.mask)
		}
		got := tlsVersionsFromMask(tc.mask)
		want := slices.Clone(tc.versions)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("tlsVersionsFromMask(%d) = %v, want %v", tc.mask, got, want)
		}
	}
	if got := tlsVersionsFromMask(2); !slices.Contains(got, "ssl3") {
		t.Errorf("mask without the SSLv3 bit should report ssl3, got %v", got)
	}
}

func TestCertHelpers(t *testing.T) {
	a := genCert(t, "a.example.test", false, nil, true)
	b := genCert(t, "b.example.test", false, nil, false)

	if !sameCert(a.CertPEM, "\n"+a.CertPEM+"\n") {
		t.Error("formatting differences should not matter")
	}
	if sameCert(a.CertPEM, b.CertPEM) {
		t.Error("different certificates compared equal")
	}
	if _, err := parseSingleCert(a.CertPEM + b.CertPEM); err == nil {
		t.Error("a chain should be rejected")
	}
	if _, err := parseSingleCert(a.CertPEM + a.KeyPEM); err == nil {
		t.Error("a key in the certificate attribute should be rejected")
	}
	info := certInfoFrom(b.CertPEM)
	if info.KeyType.ValueString() != "ECC" || info.Subject.ValueString() != "CN=b.example.test" || len(info.DNSNames.Elements()) != 1 {
		t.Errorf("unexpected info %+v", info)
	}
	if certInfoFrom(a.CertPEM).KeyType.ValueString() != "RSA" {
		t.Error("RSA key type")
	}
}
