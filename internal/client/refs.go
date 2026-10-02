package client

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// LoadMaster virtual service indexes are not stable identities: any global
// configuration change (a cipher set or certificate change, for example)
// renumbers every virtual service, top-level ones first and SubVSs after,
// closing gaps. Real server indexes (RsIndex), including SubVS slots in their
// parent, are not renumbered.
//
// So objects are identified by what the LoadMaster does keep stable, and the
// current index is looked up immediately before each use:
//
//	virtual service  <protocol>/<address>/<port>         tcp/10.0.0.1/443
//	SubVS            <parent ref>/sub/<slot RsIndex>      tcp/10.0.0.1/443/sub/2
//	real server      <owner ref>/rs/<RsIndex>             tcp/10.0.0.1/443/rs/5

// VSRef identifies a virtual service, or a SubVS when SubSlot is non-zero.
type VSRef struct {
	Protocol string
	Address  string
	Port     string
	SubSlot  int
}

func (r VSRef) String() string {
	s := r.Protocol + "/" + r.Address + "/" + r.Port
	if r.SubSlot != 0 {
		s += "/sub/" + strconv.Itoa(r.SubSlot)
	}
	return s
}

// Parent returns the reference of a SubVS's parent.
func (r VSRef) Parent() VSRef {
	return VSRef{Protocol: r.Protocol, Address: r.Address, Port: r.Port}
}

// ParseVSRef parses a virtual service or SubVS reference.
func ParseVSRef(s string) (VSRef, error) {
	parts := strings.Split(s, "/")
	if len(parts) != 3 && len(parts) != 5 {
		return VSRef{}, fmt.Errorf("%q is not a virtual service reference (protocol/address/port[/sub/slot])", s)
	}
	r := VSRef{Protocol: parts[0], Address: parts[1], Port: parts[2]}
	if r.Protocol == "" || r.Address == "" || r.Port == "" {
		return VSRef{}, fmt.Errorf("%q is not a virtual service reference (protocol/address/port[/sub/slot])", s)
	}
	if len(parts) == 5 {
		slot, err := strconv.Atoi(parts[4])
		if parts[3] != "sub" || err != nil || slot <= 0 {
			return VSRef{}, fmt.Errorf("%q is not a SubVS reference (protocol/address/port/sub/slot)", s)
		}
		r.SubSlot = slot
	}
	return r, nil
}

// RSRef identifies a real server on a virtual service or SubVS.
type RSRef struct {
	VS      VSRef
	RsIndex int
}

func (r RSRef) String() string {
	return r.VS.String() + "/rs/" + strconv.Itoa(r.RsIndex)
}

// ParseRSRef parses a real server reference.
func ParseRSRef(s string) (RSRef, error) {
	i := strings.LastIndex(s, "/rs/")
	if i < 0 {
		return RSRef{}, fmt.Errorf("%q is not a real server reference (<virtual service reference>/rs/<index>)", s)
	}
	vs, err := ParseVSRef(s[:i])
	if err != nil {
		return RSRef{}, err
	}
	n, err := strconv.Atoi(s[i+len("/rs/"):])
	if err != nil || n <= 0 {
		return RSRef{}, fmt.Errorf("%q is not a real server reference (<virtual service reference>/rs/<index>)", s)
	}
	return RSRef{VS: vs, RsIndex: n}, nil
}

// RefOf returns the reference of a top-level virtual service.
func RefOf(vs *VirtualService) VSRef {
	return VSRef{Protocol: vs.Protocol, Address: vs.VSAddress, Port: vs.VSPort}
}

// ResolveVS returns the virtual service or SubVS a reference points to, with
// its current index. Use IsNotFound to detect a missing one.
func (c *Client) ResolveVS(ctx context.Context, ref VSRef) (*VirtualService, error) {
	var parent VirtualService
	params := map[string]any{"vs": ref.Address, "port": ref.Port, "prot": ref.Protocol}
	if err := c.Do(ctx, "showvs", params, &parent); err != nil {
		return nil, err
	}
	if ref.SubSlot == 0 {
		return &parent, nil
	}
	for _, s := range parent.SubVS {
		if s.RsIndex == ref.SubSlot {
			sub, err := c.GetVirtualService(ctx, s.VSIndex)
			if err != nil {
				return nil, err
			}
			// Guard against a renumber between the two reads.
			if sub.MasterVSID != parent.Index {
				return nil, fmt.Errorf("SubVS %s moved while being looked up; try again", ref)
			}
			return sub, nil
		}
	}
	return nil, &APIError{Code: 422, Message: fmt.Sprintf("Unknown SubVS %s", ref)}
}

// RefForIndex works out the stable reference of the virtual service or SubVS
// currently at index. It's for imports and state upgrades, which start from
// an index.
func (c *Client) RefForIndex(ctx context.Context, index int) (VSRef, error) {
	vs, err := c.GetVirtualService(ctx, index)
	if err != nil {
		return VSRef{}, err
	}
	if vs.MasterVSID == 0 {
		return RefOf(vs), nil
	}
	parent, err := c.GetVirtualService(ctx, vs.MasterVSID)
	if err != nil {
		return VSRef{}, err
	}
	for _, s := range parent.SubVS {
		if s.VSIndex == index {
			ref := RefOf(parent)
			ref.SubSlot = s.RsIndex
			return ref, nil
		}
	}
	return VSRef{}, fmt.Errorf("virtual service %d says its parent is %d, but the parent doesn't list it", index, vs.MasterVSID)
}

// CheckSame confirms that vs (as returned by a write) is the object ref
// points to, so a renumber between lookup and write is caught rather than
// silently applied to another virtual service.
func CheckSame(ref VSRef, vs *VirtualService, parentIndex int) error {
	if ref.SubSlot == 0 {
		if got := RefOf(vs); got != ref {
			return fmt.Errorf("the LoadMaster changed %s instead of %s (indexes were renumbered mid-operation); check it and try again", got, ref)
		}
		return nil
	}
	if vs.MasterVSID != parentIndex {
		return fmt.Errorf("the LoadMaster changed a different SubVS than %s (indexes were renumbered mid-operation); check it and try again", ref)
	}
	return nil
}
