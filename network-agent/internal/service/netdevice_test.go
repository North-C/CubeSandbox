// Copyright (c) 2024 Tencent Inc.
// SPDX-License-Identifier: Apache-2.0
//

package service

import (
	"net"
	"testing"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func TestUsableGatewayNeighborAcceptsStableStates(t *testing.T) {
	states := []int{
		unix.NUD_REACHABLE,
		unix.NUD_STALE,
		unix.NUD_DELAY,
		unix.NUD_PROBE,
		unix.NUD_PERMANENT,
	}

	for _, state := range states {
		neigh := netlink.Neigh{
			Family:       netlink.FAMILY_V4,
			State:        state,
			HardwareAddr: net.HardwareAddr{0x02, 0xaa, 0xbb, 0xcc, 0xdd, 0xee},
		}
		if !usableGatewayNeighbor(neigh) {
			t.Fatalf("state=%d should be usable", state)
		}
	}
}

func TestUsableGatewayNeighborRejectsUnusableStates(t *testing.T) {
	tests := []netlink.Neigh{
		{
			Family:       netlink.FAMILY_V6,
			State:        unix.NUD_REACHABLE,
			HardwareAddr: net.HardwareAddr{0x02, 0xaa, 0xbb, 0xcc, 0xdd, 0xee},
		},
		{
			Family:       netlink.FAMILY_V4,
			State:        unix.NUD_REACHABLE,
			HardwareAddr: nil,
		},
		{
			Family:       netlink.FAMILY_V4,
			State:        unix.NUD_FAILED,
			HardwareAddr: net.HardwareAddr{0x02, 0xaa, 0xbb, 0xcc, 0xdd, 0xee},
		},
		{
			Family:       netlink.FAMILY_V4,
			State:        unix.NUD_INCOMPLETE,
			HardwareAddr: net.HardwareAddr{0x02, 0xaa, 0xbb, 0xcc, 0xdd, 0xee},
		},
	}

	for _, neigh := range tests {
		if usableGatewayNeighbor(neigh) {
			t.Fatalf("neighbor=%+v should not be usable", neigh)
		}
	}
}

func TestEnsureRouteToCubeDev(t *testing.T) {
	originalReplace := netlinkRouteReplace
	originalList := netlinkRouteListFiltered
	defer func() {
		netlinkRouteReplace = originalReplace
		netlinkRouteListFiltered = originalList
	}()

	var got *netlink.Route
	netlinkRouteListFiltered = func(_ int, _ *netlink.Route, _ uint64) ([]netlink.Route, error) {
		return nil, nil
	}
	netlinkRouteReplace = func(route *netlink.Route) error {
		got = route
		return nil
	}

	err := ensureRouteToCubeDev("192.168.0.0/18", &cubeDev{
		Index: 7,
		Name:  cubeDevName,
		IP:    net.ParseIP("192.168.0.1").To4(),
	})
	if err != nil {
		t.Fatalf("ensureRouteToCubeDev error=%v", err)
	}
	if got == nil {
		t.Fatal("route=nil, want route to be installed")
	}
	if got.LinkIndex != 7 {
		t.Fatalf("LinkIndex=%d, want 7", got.LinkIndex)
	}
	if got.Dst == nil || got.Dst.String() != "192.168.0.0/18" {
		t.Fatalf("Dst=%v, want 192.168.0.0/18", got.Dst)
	}
	if got.Scope != netlink.SCOPE_LINK {
		t.Fatalf("Scope=%d, want %d", got.Scope, netlink.SCOPE_LINK)
	}
	if got.Protocol != unix.RTPROT_STATIC {
		t.Fatalf("Protocol=%d, want %d", got.Protocol, unix.RTPROT_STATIC)
	}
}

func TestEnsureRouteToCubeDevSkipsExistingRoute(t *testing.T) {
	originalReplace := netlinkRouteReplace
	originalList := netlinkRouteListFiltered
	defer func() {
		netlinkRouteReplace = originalReplace
		netlinkRouteListFiltered = originalList
	}()

	netlinkRouteListFiltered = func(_ int, route *netlink.Route, _ uint64) ([]netlink.Route, error) {
		return []netlink.Route{{
			LinkIndex: route.LinkIndex,
			Dst:       route.Dst,
			Scope:     route.Scope,
		}}, nil
	}
	netlinkRouteReplace = func(_ *netlink.Route) error {
		t.Fatal("route replace should not be called when route already exists")
		return nil
	}

	err := ensureRouteToCubeDev("192.168.0.0/18", &cubeDev{Index: 7, Name: cubeDevName})
	if err != nil {
		t.Fatalf("ensureRouteToCubeDev error=%v", err)
	}
}

func TestEnsureRouteToCubeDevRejectsInvalidCIDR(t *testing.T) {
	err := ensureRouteToCubeDev("bad-cidr", &cubeDev{Index: 7, Name: cubeDevName})
	if err == nil {
		t.Fatal("ensureRouteToCubeDev error=nil, want invalid cidr")
	}
}

func TestEnsureRouteToCubeDevRequiresDevice(t *testing.T) {
	err := ensureRouteToCubeDev("192.168.0.0/18", nil)
	if err == nil {
		t.Fatal("ensureRouteToCubeDev error=nil, want missing device")
	}
}
