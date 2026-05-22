// Copyright (c) 2024 Tencent Inc.
// SPDX-License-Identifier: Apache-2.0
//

package network

import (
	"net"
	"strconv"
	"strings"
	"sync"
)

type IPAllocator struct {
	sync.Mutex
	maxIdx    int
	cidr      string
	mask      int
	gwIP      net.IP
	size      int
	startIdx  int
	usedIPNum int
	bitmap    []byte
}

func NewAllocator(cidr string) (*IPAllocator, error) {
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
	allocator := &IPAllocator{
		mask:      mask,
		cidr:      cidr,
		size:      size,
		maxIdx:    1,
		usedIPNum: 0,
		bitmap:    make([]byte, byteNum),
	}
	startIdx := allocator.ip2Idx(startIP)
	allocator.startIdx = startIdx
	allocator.setUsed(0)
	allocator.setUsed(1)
	allocator.setUsed(size - 1)

	gwIP := allocator.idx2IP(1)
	allocator.gwIP = gwIP

	return allocator, nil
}

func (a *IPAllocator) GatewayIP() net.IP {
	return a.gwIP
}

func (a *IPAllocator) exist(ip net.IP) (bool, error) {
	idx := a.ip2Idx(ip) - a.startIdx
	if idx < 0 || idx >= a.size {
		return false, ErrNotRangeIP
	}

	return a.existIdx(idx), nil
}

func (a *IPAllocator) existIdx(idx int) bool {
	return a.bitmap[idx/8]&byte(1<<(idx%8)) > 0
}

func (a *IPAllocator) setUsed(idx int) {
	a.usedIPNum++
	a.bitmap[idx/8] = a.bitmap[idx/8] | (1 << (idx % 8))
}

func (a *IPAllocator) setUnUsed(idx int) {
	a.usedIPNum--
	a.bitmap[idx/8] = a.bitmap[idx/8] &^ (1 << (idx % 8))
}

func (a *IPAllocator) ip2Idx(ip net.IP) int {
	ip = ip.To4()
	bytes := []byte(ip)

	return int(bytes[0])*256*256*256 + int(bytes[1])*256*256 + int(bytes[2])*256 + int(bytes[3])
}

func (a *IPAllocator) idx2IP(idx int) net.IP {
	targetIdx := idx + a.startIdx
	bytes := make([]byte, 4)
	bytes[0] = byte(targetIdx / (256 * 256 * 256))
	bytes[1] = byte(targetIdx % (256 * 256 * 256) / (256 * 256))
	bytes[2] = byte(targetIdx % (256 * 256) / 256)
	bytes[3] = byte(targetIdx % 256)

	return net.IPv4(bytes[0], bytes[1], bytes[2], bytes[3]).To4()
}

func (a *IPAllocator) Allocate() (net.IP, error) {
	a.Mutex.Lock()
	defer a.Mutex.Unlock()
	if a.usedIPNum >= a.size {
		return nil, ErrIPExhausted
	}
	for {
		a.maxIdx = (a.maxIdx + 1) % a.size
		idx := a.maxIdx
		exist := a.existIdx(idx)
		if !exist {
			a.setUsed(idx)
			return a.idx2IP(idx), nil
		}
	}
}

func (a *IPAllocator) Release(ip net.IP) {
	a.Mutex.Lock()
	defer a.Mutex.Unlock()
	idx := a.ip2Idx(ip) - a.startIdx
	exist := a.existIdx(idx)
	if exist {
		a.setUnUsed(idx)
	}
}

func (a *IPAllocator) Assign(ip net.IP) {
	a.Mutex.Lock()
	defer a.Mutex.Unlock()
	idx := a.ip2Idx(ip) - a.startIdx
	if idx >= 0 && idx < a.size {
		exist := a.existIdx(idx)
		if !exist {
			a.setUsed(idx)
		}
	}
	if idx > a.maxIdx {
		a.maxIdx = idx
	}
}
