package client

import (
	"context"
	"encoding/base64"
	"strings"
)

// Certificate is a certificate entry from listcert or listintermediate.
type Certificate struct {
	Name string `json:"name"`
	Type string `json:"type"` // RSA or ECC
}

type certListResponse struct {
	Cert []Certificate `json:"cert"`
}

type readCertResponse struct {
	Certificate string `json:"certificate"` // PEM, leaf only, no key
}

// ListCertificates returns the installed certificates (not intermediates).
func (c *Client) ListCertificates(ctx context.Context) ([]Certificate, error) {
	var out certListResponse
	err := c.Do(ctx, "listcert", nil, &out)
	return out.Cert, err
}

// ReadCertificate returns a certificate's PEM. Only the leaf is stored, never
// the key. Use IsNotFound to detect a missing one ("Unknown certificate").
func (c *Client) ReadCertificate(ctx context.Context, name string) (string, error) {
	var out readCertResponse
	err := c.Do(ctx, "readcert", map[string]any{"cert": name}, &out)
	return out.Certificate, err
}

// AddCertificate installs a certificate from its PEM certificate and private
// key. With replace, an existing certificate of the same name is replaced in
// place, keeping its virtual service assignments. The LoadMaster keeps only
// the first certificate of a chain; intermediates are installed separately.
func (c *Client) AddCertificate(ctx context.Context, name, certPEM, keyPEM string, replace bool) error {
	params := map[string]any{
		"cert": name,
		"data": base64.StdEncoding.EncodeToString([]byte(strings.TrimSpace(certPEM) + "\n" + strings.TrimSpace(keyPEM) + "\n")),
	}
	if replace {
		params["replace"] = "1"
	}
	return c.globalWrite(ctx, "addcert", params)
}

// DeleteCertificate deletes a certificate. The LoadMaster refuses while a
// virtual service uses it ("Certfile still associated with a VS").
func (c *Client) DeleteCertificate(ctx context.Context, name string) error {
	return c.globalWrite(ctx, "delcert", map[string]any{"cert": name})
}

// ListIntermediates returns the installed intermediate certificates, which
// also serve as the trust store for client certificates.
func (c *Client) ListIntermediates(ctx context.Context) ([]Certificate, error) {
	var out certListResponse
	err := c.Do(ctx, "listintermediate", nil, &out)
	return out.Cert, err
}

// ReadIntermediate returns an intermediate certificate's PEM.
func (c *Client) ReadIntermediate(ctx context.Context, name string) (string, error) {
	var out readCertResponse
	err := c.Do(ctx, "readintermediate", map[string]any{"cert": name}, &out)
	return out.Certificate, err
}

// AddIntermediate installs an intermediate certificate. Intermediates can't
// be replaced in place; delete and re-add instead.
func (c *Client) AddIntermediate(ctx context.Context, name, certPEM string) error {
	return c.globalWrite(ctx, "addintermediate", map[string]any{
		"cert": name,
		"data": base64.StdEncoding.EncodeToString([]byte(strings.TrimSpace(certPEM) + "\n")),
	})
}

// DeleteIntermediate deletes an intermediate certificate.
func (c *Client) DeleteIntermediate(ctx context.Context, name string) error {
	return c.globalWrite(ctx, "delintermediate", map[string]any{"cert": name})
}

type cipherSetResponse struct {
	CipherSet string `json:"cipherset"`
}

// GetCipherSet returns a cipher set's ciphers. Use IsNotFound to detect a
// missing one ("Unknown cipher set").
func (c *Client) GetCipherSet(ctx context.Context, name string) ([]string, error) {
	var out cipherSetResponse
	if err := c.Do(ctx, "getcipherset", map[string]any{"name": name}, &out); err != nil {
		return nil, err
	}
	return strings.Split(out.CipherSet, ":"), nil
}

// SetCipherSet creates or replaces a custom cipher set. The LoadMaster
// rejects unknown ciphers and changes to built-in sets.
func (c *Client) SetCipherSet(ctx context.Context, name string, ciphers []string) error {
	return c.globalWrite(ctx, "modifycipherset", map[string]any{"name": name, "value": strings.Join(ciphers, ":")})
}

// DeleteCipherSet deletes a custom cipher set. The LoadMaster refuses while a
// virtual service uses it ("Cipher set in use").
func (c *Client) DeleteCipherSet(ctx context.Context, name string) error {
	return c.globalWrite(ctx, "delcipherset", map[string]any{"name": name})
}

// noAdminCertificate is what "get param=admincert" reports when none is set.
const noAdminCertificate = "No Admin Certificate assigned"

type adminCertResponse struct {
	AdminCert string `json:"admincert"`
}

// GetAdminCertificate returns the name of the certificate the web UI and API
// use, or "" when none is assigned.
func (c *Client) GetAdminCertificate(ctx context.Context) (string, error) {
	var out adminCertResponse
	if err := c.Do(ctx, "get", map[string]any{"param": "admincert"}, &out); err != nil {
		return "", err
	}
	if out.AdminCert == noAdminCertificate {
		return "", nil
	}
	return out.AdminCert, nil
}

// SetAdminCertificate makes the named certificate the one the web UI and API
// use. Replacing a certificate in place (AddCertificate with replace) drops
// this assignment, while the web server keeps serving the old certificate from
// memory, so callers re-assign after every replacement. Needs a user with All
// Permissions.
func (c *Client) SetAdminCertificate(ctx context.Context, name string) error {
	return c.globalWrite(ctx, "set", map[string]any{"param": "admincert", "value": name})
}
