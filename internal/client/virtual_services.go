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
