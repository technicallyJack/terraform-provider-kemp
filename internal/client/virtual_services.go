package client

import (
	"context"
	"fmt"
	"strconv"
)

// VirtualService is a subset of the fields returned by listvs/showvs.
// Extend as resources need more attributes.
type VirtualService struct {
	Index      int    `json:"Index"`
	NickName   string `json:"NickName"`
	VSAddress  string `json:"VSAddress"`
	VSPort     string `json:"VSPort"`
	Protocol   string `json:"Protocol"`
	VSType     string `json:"VStype"`
	Status     string `json:"Status"` // only reliable from listvs: in showvs the API's own "status" shadows it
	Enable     bool   `json:"Enable"`
	MasterVSID int    `json:"MasterVSID"` // parent index for SubVSs, 0 for top-level

	Schedule       string `json:"Schedule"`
	CheckType      string `json:"CheckType"`
	CheckPort      string `json:"CheckPort"`   // "0" means use each real server's port
	CheckURL       string `json:"CheckUrl"`    // omitted by the API when empty
	CheckHost      string `json:"CheckHost"`   // omitted by the API when empty
	CheckUseGet    int    `json:"CheckUseGet"` // 0 = HEAD, 1 = GET, 2 = POST
	CheckUseHTTP11 bool   `json:"CheckUse1.1"`

	ChkInterval          int    `json:"ChkInterval"`   // seconds, 9-120; 0 = global default (can't be set back)
	ChkTimeout           int    `json:"ChkTimeout"`    // seconds, 4-60; 0 = global default
	ChkRetryCount        int    `json:"ChkRetryCount"` // 2-15; 0 = global default
	CheckPattern         string `json:"CheckPattern"`  // omitted when empty
	CheckPostData        string `json:"CheckPostData"`
	CheckHeaders         string `json:"CheckHeaders"` // space-separated Name:value pairs
	EnhancedHealthChecks bool   `json:"EnhancedHealthChecks"`
	RsMinimum            int    `json:"RsMinimum"` // needs EnhancedHealthChecks; 1 is stored as 0

	Transparent       bool `json:"Transparent"`
	SubnetOriginating bool `json:"SubnetOriginating"`
	AddVia            int  `json:"AddVia"`   // 0-6, which X-Forwarded-For/Via/X-ClientSide headers to add
	Idletime          int  `json:"Idletime"` // seconds, 1-86400 (0 is stored as the default, 660)
	Cache             bool `json:"Cache"`
	Compress          bool `json:"Compress"`
	ForceL7           bool `json:"ForceL7"` // off means ForceL4; only matters for gen services

	Persist        string `json:"Persist"`        // omitted by the API when persistence is off
	PersistTimeout string `json:"PersistTimeout"` // seconds; "0" when off
	Cookie         string `json:"Cookie"`         // cookie name, or header name for header persistence
	QueryTag       string `json:"QueryTag"`       // query parameter for query-hash persistence

	// SSL settings; only meaningful while SSLAcceleration is on (turning it
	// off clears CertFile, the rest keep their values).
	SSLAcceleration bool   `json:"SSLAcceleration"`
	CertFile        string `json:"CertFile"` // space-separated certificate names
	TlsType         string `json:"TlsType"`  // bitmask of *disabled* versions: 1 SSLv3, 2 TLS1.0, 4 TLS1.1, 8 TLS1.2, 16 TLS1.3
	CipherSet       string `json:"CipherSet"`
	SSLReencrypt    bool   `json:"SSLReencrypt"`
	AllowHTTP2      bool   `json:"AllowHTTP2"`
	PassSni         bool   `json:"PassSni"`
	ClientCert      int    `json:"ClientCert"` // 0 none, 1-6 required (2-6 also pass it to real servers)

	RequestRules    []string `json:"RequestRules"` // omitted by the API when empty
	ResponseRules   []string `json:"ResponseRules"`
	PreProcessRules []string `json:"PreProcessRules"`
	MatchBodyRules  []string `json:"MatchBodyRules"` // response body rules; changing the service type clears them

	SubVS []SubVSSlot `json:"SubVS"` // only on parents
}

// VirtualServiceParams holds the settable attributes for addvs/modvs.
// Nil fields are left out of the request.
type VirtualServiceParams struct {
	Address  *string
	Port     *string
	Protocol *string
	NickName *string
	Enable   *bool
	VSType   *string

	Schedule       *string
	CheckType      *string
	CheckPort      *string
	CheckURL       *string // "" clears it
	CheckHost      *string // "" clears it
	CheckUseGet    *int
	CheckUseHTTP11 *bool

	ChkInterval          *int
	ChkTimeout           *int
	ChkRetryCount        *int
	CheckPattern         *string // "" clears it
	CheckPostData        *string
	CheckHeaders         *string
	EnhancedHealthChecks *bool
	RsMinimum            *int

	Transparent       *bool
	SubnetOriginating *bool
	AddVia            *int
	Idletime          *int
	Cache             *bool
	Compress          *bool
	ForceL7           *bool

	// Persist "none" turns persistence off. Don't send PersistTimeout with
	// it: any timeout of 60+ while persistence is off enables source IP
	// persistence, and 1-59 is silently stored as 0 (off).
	Persist        *string
	PersistTimeout *string
	Cookie         *string // "" clears it
	QueryTag       *string // "" clears it

	SSLAcceleration *bool
	CertFile        *string
	TlsType         *int
	CipherSet       *string
	SSLReencrypt    *bool
	AllowHTTP2      *bool
	PassSni         *bool
	ClientCert      *int
}

func (p VirtualServiceParams) toMap() map[string]any {
	m := map[string]any{}
	if p.NickName != nil {
		m["NickName"] = *p.NickName
	}
	if p.Enable != nil {
		m["Enable"] = *p.Enable
	}
	if p.VSType != nil {
		m["VStype"] = *p.VSType
	}
	if p.Schedule != nil {
		m["Schedule"] = *p.Schedule
	}
	if p.CheckType != nil {
		m["CheckType"] = *p.CheckType
	}
	if p.CheckPort != nil {
		m["CheckPort"] = *p.CheckPort
	}
	if p.CheckURL != nil {
		m["CheckUrl"] = *p.CheckURL
	}
	if p.CheckHost != nil {
		m["CheckHost"] = *p.CheckHost
	}
	if p.CheckUseGet != nil {
		m["CheckUseGet"] = strconv.Itoa(*p.CheckUseGet)
	}
	if p.CheckUseHTTP11 != nil {
		m["CheckUse1.1"] = *p.CheckUseHTTP11
	}
	if p.Persist != nil {
		m["Persist"] = *p.Persist
	}
	if p.PersistTimeout != nil {
		m["PersistTimeout"] = *p.PersistTimeout
	}
	if p.Cookie != nil {
		m["Cookie"] = *p.Cookie
	}
	if p.QueryTag != nil {
		m["QueryTag"] = *p.QueryTag
	}
	setBool := func(k string, v *bool) {
		if v != nil {
			m[k] = *v
		}
	}
	setInt := func(k string, v *int) {
		if v != nil {
			m[k] = strconv.Itoa(*v)
		}
	}
	setString := func(k string, v *string) {
		if v != nil {
			m[k] = *v
		}
	}
	setInt("ChkInterval", p.ChkInterval)
	setInt("ChkTimeout", p.ChkTimeout)
	setInt("ChkRetryCount", p.ChkRetryCount)
	setString("CheckPattern", p.CheckPattern)
	setString("CheckPostData", p.CheckPostData)
	setString("CheckHeaders", p.CheckHeaders)
	setBool("EnhancedHealthChecks", p.EnhancedHealthChecks)
	setInt("RsMinimum", p.RsMinimum)
	setBool("Transparent", p.Transparent)
	setBool("SubnetOriginating", p.SubnetOriginating)
	setBool("Cache", p.Cache)
	setBool("Compress", p.Compress)
	setBool("ForceL7", p.ForceL7)
	if p.AddVia != nil {
		m["AddVia"] = strconv.Itoa(*p.AddVia)
	}
	if p.Idletime != nil {
		m["Idletime"] = strconv.Itoa(*p.Idletime)
	}
	setBool("SSLAcceleration", p.SSLAcceleration)
	setBool("SSLReencrypt", p.SSLReencrypt)
	setBool("AllowHTTP2", p.AllowHTTP2)
	setBool("PassSni", p.PassSni)
	if p.CertFile != nil {
		m["CertFile"] = *p.CertFile
	}
	if p.TlsType != nil {
		m["TlsType"] = strconv.Itoa(*p.TlsType)
	}
	if p.CipherSet != nil {
		m["CipherSet"] = *p.CipherSet
	}
	if p.ClientCert != nil {
		m["ClientCert"] = strconv.Itoa(*p.ClientCert)
	}
	return m
}

type listVSResponse struct {
	VS []VirtualService `json:"VS"`
}

// ListVirtualServices returns all virtual services on the LoadMaster,
// including SubVSs (which have a non-zero MasterVSID).
func (c *Client) ListVirtualServices(ctx context.Context) ([]VirtualService, error) {
	var out listVSResponse
	if err := c.Do(ctx, "listvs", nil, &out); err != nil {
		return nil, err
	}
	return out.VS, nil
}

// GetVirtualService returns the virtual service with the given index.
// Use IsNotFound to detect a missing VS.
func (c *Client) GetVirtualService(ctx context.Context, index int) (*VirtualService, error) {
	var vs VirtualService
	if err := c.Do(ctx, "showvs", map[string]any{"vs": strconv.Itoa(index)}, &vs); err != nil {
		return nil, err
	}
	return &vs, nil
}

// CreateVirtualService creates a virtual service. Address, Port and
// Protocol are required. The result comes from looking the new virtual
// service up by those, not from the addvs response, which concurrent creates
// can get wrong.
func (c *Client) CreateVirtualService(ctx context.Context, p VirtualServiceParams) (*VirtualService, error) {
	if p.Address == nil || p.Port == nil || p.Protocol == nil {
		return nil, fmt.Errorf("address, port and protocol are required to create a virtual service")
	}
	params := p.toMap()
	params["vs"], params["port"], params["prot"] = *p.Address, *p.Port, *p.Protocol

	c.createMu.Lock()
	defer c.createMu.Unlock()
	if err := c.Do(ctx, "addvs", params, nil); err != nil {
		return nil, err
	}
	ref := VSRef{Protocol: *p.Protocol, Address: *p.Address, Port: *p.Port}
	vs, err := c.ResolveVS(ctx, ref)
	if IsNotFound(err) {
		return nil, fmt.Errorf("the LoadMaster reported creating virtual service %s, but it doesn't exist", ref)
	}
	return vs, err
}

// UpdateVirtualService modifies the virtual service with the given index.
func (c *Client) UpdateVirtualService(ctx context.Context, index int, p VirtualServiceParams) (*VirtualService, error) {
	params := p.toMap()
	params["vs"] = strconv.Itoa(index)
	if p.Address != nil {
		params["VSAddress"] = *p.Address
	}
	if p.Port != nil {
		params["VSPort"] = *p.Port
	}

	var vs VirtualService
	if err := c.Do(ctx, "modvs", params, &vs); err != nil {
		return nil, err
	}
	return &vs, nil
}

// DeleteVirtualService deletes the virtual service with the given index.
func (c *Client) DeleteVirtualService(ctx context.Context, index int) error {
	return c.Do(ctx, "delvs", map[string]any{"vs": strconv.Itoa(index)}, nil)
}
