package client

import (
	"context"
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
	Status     string `json:"Status"`
	Enable     bool   `json:"Enable"`
	MasterVSID int    `json:"MasterVSID"` // parent index for SubVSs, 0 for top-level

	Schedule       string `json:"Schedule"`
	CheckType      string `json:"CheckType"`
	CheckPort      string `json:"CheckPort"`   // "0" means use each real server's port
	CheckURL       string `json:"CheckUrl"`    // omitted by the API when empty
	CheckHost      string `json:"CheckHost"`   // omitted by the API when empty
	CheckUseGet    int    `json:"CheckUseGet"` // 0 = HEAD, 1 = GET, 2 = POST
	CheckUseHTTP11 bool   `json:"CheckUse1.1"`

	Persist        string `json:"Persist"`        // omitted by the API when persistence is off
	PersistTimeout string `json:"PersistTimeout"` // seconds; "0" when off
	Cookie         string `json:"Cookie"`         // cookie name, or header name for header persistence
	QueryTag       string `json:"QueryTag"`       // query parameter for query-hash persistence

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

	// Persist "none" turns persistence off. Don't send PersistTimeout with
	// it: any timeout of 60+ while persistence is off enables source IP
	// persistence, and 1-59 is silently stored as 0 (off).
	Persist        *string
	PersistTimeout *string
	Cookie         *string // "" clears it
	QueryTag       *string // "" clears it
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

// CreateVirtualService creates a virtual service. Address and Port are
// required; Protocol defaults to tcp on the LoadMaster.
func (c *Client) CreateVirtualService(ctx context.Context, p VirtualServiceParams) (*VirtualService, error) {
	params := p.toMap()
	if p.Address != nil {
		params["vs"] = *p.Address
	}
	if p.Port != nil {
		params["port"] = *p.Port
	}
	if p.Protocol != nil {
		params["prot"] = *p.Protocol
	}

	var vs VirtualService
	if err := c.Do(ctx, "addvs", params, &vs); err != nil {
		return nil, err
	}
	return &vs, nil
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
