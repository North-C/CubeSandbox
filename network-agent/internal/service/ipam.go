// Copyright (c) 2024 Tencent Inc.
// SPDX-License-Identifier: Apache-2.0
//

package service

import (
	"net"
	"strconv"
	"strings"
	"sync"
)

type ipAllocator struct {
	sync.Mutex
	maxIdx    int
	mask      int
	gwIP      net.IP
	size      int
	startIdx  int
	usedIPNum int
	bitmap    []byte
}

func newIPAllocator(cidr string) (*ipAllocator, error) {
	i := strings.Index(cidr, "/")
	if i < 0 {
		return nil, &net.ParseError{Type: "cidr address", Text: cidr}
	}
	addr, m := cidr[:i], cidr[i+1:]
	mask, err := strconv.Atoi(m)
	if err != nil {
		return nil, &net.ParseError{Type: "cidr mask fail", Text: cidr}
	}
	startIP := net.ParseIP(addr).To4()
	size := 1 << (32 - mask)
	byteNum := size / 8
	if size%8 != 0 {
		byteNum++
	}
	allocator := &ipAllocator{
		mask:      mask,
		size:      size,
		maxIdx:    1,
		bitmap:    make([]byte, byteNum),
		usedIPNum: 0,
	}
	allocator.startIdx = allocator.ip2Idx(startIP)
	allocator.setUsed(0)
	allocator.setUsed(1)
	allocator.setUsed(size - 1)
	allocator.gwIP = allocator.idx2IP(1)
	return allocator, nil
}

func (a *ipAllocator) GatewayIP() net.IP {
	return a.gwIP
}

func (a *ipAllocator) setUsed(idx int) {
	a.usedIPNum++
	a.bitmap[idx/8] = a.bitmap[idx/8] | (1 << (idx % 8))
}

func (a *ipAllocator) setUnused(idx int) {
	a.usedIPNum--
	a.bitmap[idx/8] = a.bitmap[idx/8] &^ (1 << (idx % 8))
}

func (a *ipAllocator) ip2Idx(ip net.IP) int {
	ip = ip.To4()
	bytes := []byte(ip)
	return int(bytes[0])*256*256*256 + int(bytes[1])*256*256 + int(bytes[2])*256 + int(bytes[3])
}

func (a *ipAllocator) idx2IP(idx int) net.IP {
	targetIdx := idx + a.startIdx
	bytes := make([]byte, 4)
	bytes[0] = byte(targetIdx / (256 * 256 * 256))
	bytes[1] = byte(targetIdx % (256 * 256 * 256) / (256 * 256))
	bytes[2] = byte(targetIdx % (256 * 256) / 256)
	bytes[3] = byte(targetIdx % 256)
	return net.IPv4(bytes[0], bytes[1], bytes[2], bytes[3]).To4()
}

func (a *ipAllocator) Allocate() (net.IP, error) {
	a.Lock()
	defer a.Unlock()
	if a.usedIPNum >= a.size {
		return nil, errIPExhausted
	}
	for {
		a.maxIdx = (a.maxIdx + 1) % a.size
		idx := a.maxIdx
		if a.bitmap[idx/8]&byte(1<<(idx%8)) == 0 {
			a.setUsed(idx)
			return a.idx2IP(idx), nil
		}
	}
}

func (a *ipAllocator) Release(ip net.IP) {
	a.Lock()
	defer a.Unlock()
	idx := a.ip2Idx(ip) - a.startIdx
	if idx < 0 || idx >= a.size {
		return
	}
	if a.bitmap[idx/8]&byte(1<<(idx%8)) > 0 {
		a.setUnused(idx)
	}
}

func (a *ipAllocator) Assign(ip net.IP) {
	a.Lock()
	defer a.Unlock()
	idx := a.ip2Idx(ip) - a.startIdx
	if idx >= 0 && idx < a.size && a.bitmap[idx/8]&byte(1<<(idx%8)) == 0 {
		a.setUsed(idx)
	}
	if idx > a.maxIdx {
		a.maxIdx = idx
	}
}
