// Copyright (c) 2024 Tencent Inc.
// SPDX-License-Identifier: Apache-2.0
//

package network

import (
	"testing"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func TestUsableGatewayNeighborAcceptsStableStates(t *testing.T) {
	for _, state := range []int{
		unix.NUD_REACHABLE,
		unix.NUD_STALE,
		unix.NUD_DELAY,
		unix.NUD_PROBE,
		unix.NUD_PERMANENT,
	} {
		neigh := netlink.Neigh{
			Family:       netlink.FAMILY_V4,
			State:        state,
			HardwareAddr: []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01},
		}
		if !usableGatewayNeighbor(neigh) {
			t.Fatalf("expected state %d to be usable", state)
		}
	}
}

func TestUsableGatewayNeighborRejectsUnusableStates(t *testing.T) {
	for _, state := range []int{unix.NUD_INCOMPLETE, unix.NUD_FAILED} {
		neigh := netlink.Neigh{
			Family:       netlink.FAMILY_V4,
			State:        state,
			HardwareAddr: []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01},
		}
		if usableGatewayNeighbor(neigh) {
			t.Fatalf("expected state %d to be unusable", state)
		}
	}

	if usableGatewayNeighbor(netlink.Neigh{Family: netlink.FAMILY_V4, State: unix.NUD_REACHABLE}) {
		t.Fatal("expected neighbor without hardware address to be unusable")
	}
	if usableGatewayNeighbor(netlink.Neigh{
		Family:       netlink.FAMILY_V6,
		State:        unix.NUD_REACHABLE,
		HardwareAddr: []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01},
	}) {
		t.Fatal("expected non-IPv4 neighbor to be unusable")
	}
}
