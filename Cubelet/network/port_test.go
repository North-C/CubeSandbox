// Copyright (c) 2024 Tencent Inc.
// SPDX-License-Identifier: Apache-2.0
//

package network

import (
	"testing"

	"github.com/agiledragon/gomonkey/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tencentcloud/CubeSandbox/Cubelet/pkg/sysctl"
	"github.com/tencentcloud/CubeSandbox/Cubelet/pkg/utils"
)

func TestInitPortAllocatorFromSysConfig(t *testing.T) {
	utils.SkipCI(t)
	p := gomonkey.ApplyFunc(sysctl.Get, func(name string) (string, error) {
		switch name {
		case "net.ipv4.ip_local_reserved_ports":
			return "8080,8888-8889,9966,9998-9999,56000", nil
		case "net.ipv4.ip_local_port_range":
			return "1024	40535", nil
		}
		return "0", nil
	})
	defer p.Reset()

	alloc, err := initPortAllocatorFromSysConfig()
	require.NoError(t, err)
	assert.False(t, alloc.Has(8080))
	assert.False(t, alloc.Has(8888))
	assert.False(t, alloc.Has(8889))
	assert.False(t, alloc.Has(9966))
	assert.False(t, alloc.Has(9998))
	assert.False(t, alloc.Has(9999))
	assert.True(t, alloc.Has(56000))
}

func TestParsePortRange(t *testing.T) {
	utils.SkipCI(t)
	p := gomonkey.ApplyFunc(sysctl.Get, func(name string) (string, error) {
		switch name {
		case "net.ipv4.ip_local_port_range":
			return "1024	40535", nil
		}
		return "0", nil
	})
	defer p.Reset()

	lower, upper, err := getLocalPortRange()
	assert.NoError(t, err)
	assert.Equal(t, uint16(1024), lower)
	assert.Equal(t, uint16(40535), upper)
}

func TestInvalidPortRange(t *testing.T) {
	utils.SkipCI(t)
	p := gomonkey.ApplyFunc(sysctl.Get, func(name string) (string, error) {
		switch name {
		case "net.ipv4.ip_local_port_range":
			return "1024	t", nil
		}
		return "0", nil
	})
	defer p.Reset()

	_, _, err := getLocalPortRange()
	assert.Error(t, err)
}

func TestParseReservedPort(t *testing.T) {
	utils.SkipCI(t)
	p := gomonkey.ApplyFunc(sysctl.Get, func(name string) (string, error) {
		switch name {
		case "net.ipv4.ip_local_reserved_ports":
			return "8080,8888-8889,9966,9998-9999,56000", nil
		}
		return "0", nil
	})
	defer p.Reset()

	list, err := getAndParseReservedPorts()
	assert.NoError(t, err)
	assert.Equal(t, []uint16{
		uint16(8080), uint16(8888), uint16(8889),
		uint16(9966), uint16(9998), uint16(9999),
		uint16(56000),
	}, list)
}
