package client

import (
	"context"
	"fmt"
	"strconv"
	"sync"
)

// SubVSSlot is a SubVS as seen from its parent: the parent treats each SubVS
// like a real server, with its own slot index (RsIndex), weight, limit and
// enabled flag.
type SubVSSlot struct {
	VSIndex int    `json:"VSIndex"` // the SubVS's own virtual service index
	RsIndex int    `json:"RsIndex"` // the slot index within the parent
	Name    string `json:"Name"`
	Weight  int    `json:"Weight"`
	Limit   int    `json:"Limit"`
	Enable  bool   `json:"Enable"`
}

// SubVSSlotParams holds the parent-side settings for a SubVS. Nil fields are
// left out of the request.
type SubVSSlotParams struct {
	Weight *int
	Limit  *int
	Enable *bool
}

func (c *Client) subVSLock(parent int) *sync.Mutex {
	c.subVSLocksMu.Lock()
	defer c.subVSLocksMu.Unlock()
	if c.subVSLocks == nil {
		c.subVSLocks = map[int]*sync.Mutex{}
	}
	if c.subVSLocks[parent] == nil {
		c.subVSLocks[parent] = &sync.Mutex{}
	}
	return c.subVSLocks[parent]
}

// CreateSubVirtualService adds a SubVS to the parent virtual service and
// returns its slot. The API returns the parent's whole SubVS list, so the new
// one is found by comparing against the list from before.
func (c *Client) CreateSubVirtualService(ctx context.Context, parent int) (*SubVSSlot, error) {
	lock := c.subVSLock(parent)
	lock.Lock()
	defer lock.Unlock()

	before, err := c.GetVirtualService(ctx, parent)
	if err != nil {
		return nil, err
	}
	existing := map[int]bool{}
	for _, s := range before.SubVS {
		existing[s.VSIndex] = true
	}

	var after VirtualService
	if err := c.Do(ctx, "modvs", map[string]any{"vs": strconv.Itoa(parent), "CreateSubVS": "1"}, &after); err != nil {
		return nil, err
	}
	for _, s := range after.SubVS {
		if !existing[s.VSIndex] {
			return &s, nil
		}
	}
	return nil, fmt.Errorf("created a SubVS on virtual service %d but could not find it in the response", parent)
}

// GetSubVSSlot returns the parent-side slot for a SubVS. Use IsNotFound to
// detect a missing parent; a missing SubVS returns an APIError with code 422.
func (c *Client) GetSubVSSlot(ctx context.Context, parent, subVS int) (*SubVSSlot, error) {
	vs, err := c.GetVirtualService(ctx, parent)
	if err != nil {
		return nil, err
	}
	for _, s := range vs.SubVS {
		if s.VSIndex == subVS {
			return &s, nil
		}
	}
	return nil, &APIError{Code: 422, Message: fmt.Sprintf("Unknown SubVS %d on virtual service %d", subVS, parent)}
}

// UpdateSubVSSlot changes the parent-side settings of a SubVS. The forwarding
// method can't be changed for SubVSs.
func (c *Client) UpdateSubVSSlot(ctx context.Context, parent, rsIndex int, p SubVSSlotParams) error {
	params := rsRef(parent, rsIndex)
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
