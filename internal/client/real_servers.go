package client

import (
	"context"
	"fmt"
	"strconv"
)

// RealServer is a backend attached to a virtual service, as returned by
// showrs/addrs and in the Rs list of showvs.
type RealServer struct {
	VSIndex int    `json:"VSIndex"`
	RsIndex int    `json:"RsIndex"`
	Addr    string `json:"Addr"`
	Port    int    `json:"Port"`
	Forward string `json:"Forward"`
	Weight  int    `json:"Weight"`
	Limit   int    `json:"Limit"`
	Enable  bool   `json:"Enable"`
	Status  string `json:"Status"`

	MatchRules []string `json:"MatchRules"` // content switching rules; omitted when empty
}

// RealServerParams holds the settable attributes for modrs. Nil fields are
// left out of the request.
type RealServerParams struct {
	Port    *int
	Forward *string
	Weight  *int
	Limit   *int
	Enable  *bool
}

type realServerResponse struct {
	Rs []RealServer `json:"Rs"`
}

func (r realServerResponse) first(cmd string) (*RealServer, error) {
	if len(r.Rs) == 0 {
		return nil, fmt.Errorf("%s returned no real server", cmd)
	}
	return &r.Rs[0], nil
}

// rsRef identifies a real server by its index, which the API expects as "!<index>".
func rsRef(vsIndex, rsIndex int) map[string]any {
	return map[string]any{"vs": strconv.Itoa(vsIndex), "rs": "!" + strconv.Itoa(rsIndex)}
}

// GetRealServer returns a real server by virtual service and real server
// index. Use IsNotFound to detect a missing VS or RS.
func (c *Client) GetRealServer(ctx context.Context, vsIndex, rsIndex int) (*RealServer, error) {
	var out realServerResponse
	if err := c.Do(ctx, "showrs", rsRef(vsIndex, rsIndex), &out); err != nil {
		return nil, err
	}
	return out.first("showrs")
}

// CreateRealServer adds a real server to a virtual service. addrs ignores
// some attributes (e.g. Forward), so set everything else with
// UpdateRealServer afterwards.
func (c *Client) CreateRealServer(ctx context.Context, vsIndex int, addr string, port int) (*RealServer, error) {
	var out realServerResponse
	params := map[string]any{"vs": strconv.Itoa(vsIndex), "rs": addr, "rsport": strconv.Itoa(port)}
	if err := c.Do(ctx, "addrs", params, &out); err != nil {
		return nil, err
	}
	return out.first("addrs")
}

// UpdateRealServer modifies a real server. The address can't be changed;
// unknown or unsupported fields are silently ignored by the LoadMaster, so
// callers should read the real server back afterwards.
func (c *Client) UpdateRealServer(ctx context.Context, vsIndex, rsIndex int, p RealServerParams) error {
	params := rsRef(vsIndex, rsIndex)
	if p.Port != nil {
		params["NewPort"] = strconv.Itoa(*p.Port)
	}
	if p.Forward != nil {
		params["Forward"] = *p.Forward
	}
	if p.Weight != nil {
		params["Weight"] = strconv.Itoa(*p.Weight)
	}
	if p.Limit != nil {
		params["Limit"] = strconv.Itoa(*p.Limit)
	}
	if p.Enable != nil {
		params["Enable"] = *p.Enable
	}
	return c.Do(ctx, "modrs", params, nil)
}

// DeleteRealServer removes a real server from its virtual service.
func (c *Client) DeleteRealServer(ctx context.Context, vsIndex, rsIndex int) error {
	return c.Do(ctx, "delrs", rsRef(vsIndex, rsIndex), nil)
}
