// Copyright (c) 2024 Tencent Inc.
// SPDX-License-Identifier: Apache-2.0
//

package network

import (
	"errors"
	"math/bits"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
)

func hanmingWeight(b byte) int {
	count := 0
	for b > 0 {
		b = b & (b - 1)
		count++
	}

	return count
}

func TestNewAllocator(t *testing.T) {
	allocator, err := NewAllocator("10.1.0.0/16")
	assert.Nil(t, err)
	count := 10000
	var ips []net.IP
	for i := 0; i < count; i++ {
		ip, err := allocator.Allocate()
		assert.Nil(t, err)
		ips = append(ips, ip)
	}
	assert.True(t, len(ips) == count)
	actual := 0
	for _, bit := range allocator.bitmap {
		actual += hanmingWeight(bit)
	}
	assert.Equal(t, count+3, actual)
	for _, ip := range ips {
		exist, err := allocator.exist(ip)
		assert.True(t, exist)
		assert.Nil(t, err)
		allocator.Release(ip)
	}
	actual = 0
	for _, bit := range allocator.bitmap {
		actual += hanmingWeight(bit)
	}
	assert.Equal(t, 3, actual)
}

func ones(bytes []byte) int {
	r := 0
	for _, b := range bytes {
		r += bits.OnesCount8(b)
	}
	return r
}

func TestIPAM(t *testing.T) {
	const (
		testCIDR        = "203.0.113.0/24"
		testMask        = 24
		testGatewayIP   = "203.0.113.1"
		testMaxIndex    = 1
		reservedIPCount = 3
	)

	ipam, err := NewAllocator(testCIDR)
	if err != nil {
		t.Fatal(err)
	}

	ip, _, err := net.ParseCIDR(testCIDR)
	if err != nil {
		t.Fatal(err)
	}
	ip = ip.To4()
	startIndex := int(ip[0])<<24 + int(ip[1])<<16 + int(ip[2])<<8 + int(ip[3])

	assert.Equal(t, testMaxIndex, ipam.maxIdx, "max index")
	assert.Equal(t, testCIDR, ipam.cidr, "wrong CIDR")
	assert.Equal(t, testMask, ipam.mask, "wrong subnet mask")
	assert.Equal(t, net.ParseIP(testGatewayIP).To4(), ipam.gwIP, "wrong gateway IP")
	assert.Equal(t, net.ParseIP(testGatewayIP).To4(), ipam.GatewayIP(), "wrong gateway IP")
	assert.Equal(t, 1<<(32-testMask), ipam.size, "wrong CIDR range")
	assert.Equal(t, startIndex, ipam.startIdx, "wrong start index")
	assert.Equal(t, reservedIPCount, ipam.usedIPNum, "wrong reserved IPs")
	assert.Equal(t, reservedIPCount, ones(ipam.bitmap), "wrong bitmap")

	var assignedIPs []net.IP
	cidrSize := 1 << (32 - testMask)
	for i := 0; i < cidrSize-reservedIPCount; i++ {
		ip, err := ipam.Allocate()
		if err != nil {
			t.Fatal(err)
		}
		assignedIPs = append(assignedIPs, ip)
	}
	assert.Equal(t, 1<<(32-testMask), ipam.usedIPNum, "wrong IP count")
	assert.Equal(t, 1<<(32-testMask), ones(ipam.bitmap), "wrong bitmap")

	_, err = ipam.Allocate()
	if !errors.Is(err, ErrIPExhausted) {
		t.Fatal(err)
	}

	for _, ip := range assignedIPs {
		ipam.Release(ip)
	}
	assert.Equal(t, reservedIPCount, ipam.usedIPNum, "wrong IP count")
	assert.Equal(t, reservedIPCount, ones(ipam.bitmap), "wrong bitmap")

	for _, ip := range assignedIPs {
		ipam.Assign(ip)
	}
	assert.Equal(t, 1<<(32-testMask), ipam.usedIPNum, "wrong IP count")
	assert.Equal(t, 1<<(32-testMask), ones(ipam.bitmap), "wrong bitmap")

	_, err = ipam.exist(net.ParseIP("203.0.114.0").To4())
	if !errors.Is(err, ErrNotRangeIP) {
		t.Fatal(err)
	}
}
