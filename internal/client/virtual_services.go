package client

import "context"

// VirtualService is a subset of the fields returned by listvs/showvs.
// Extend as resources need more attributes.
type VirtualService struct {
	Index     int    `json:"Index"`
	NickName  string `json:"NickName"`
	VSAddress string `json:"VSAddress"`
	VSPort    string `json:"VSPort"`
	Protocol  string `json:"Protocol"`
	Status    string `json:"Status"`
	Enable    bool   `json:"Enable"`
}

type listVSResponse struct {
	VS []VirtualService `json:"VS"`
}

// ListVirtualServices returns all virtual services on the LoadMaster.
func (c *Client) ListVirtualServices(ctx context.Context) ([]VirtualService, error) {
	var out listVSResponse
	if err := c.Do(ctx, "listvs", nil, &out); err != nil {
		return nil, err
	}
	return out.VS, nil
}
