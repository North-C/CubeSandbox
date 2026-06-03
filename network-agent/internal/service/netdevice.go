// Copyright (c) 2024 Tencent Inc.
// SPDX-License-Identifier: Apache-2.0
//

package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"time"
	"unsafe"

	CubeLog "github.com/tencentcloud/CubeSandbox/cubelog"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

var netlinkRouteReplace = netlink.RouteReplace
var netlinkRouteListFiltered = netlink.RouteListFiltered
var netlinkRouteList = netlink.RouteList
var netlinkLinkByIndex = netlink.LinkByIndex
var netlinkLinkByName = netlink.LinkByName
var netlinkLinkList = netlink.LinkList
var netlinkLinkDel = netlink.LinkDel
var netlinkNeighList = netlink.NeighList
var unixOpen = unix.Open
var unixClose = unix.Close
var unixIoctlIfreq = unix.IoctlIfreq
var unixIoctlSetInt = unix.IoctlSetInt

const (
	tapNamePrefix    = "z"
	cubeDevName      = "cube-dev"
	virtioNetHdrSize = 12
	txQLen           = 1000
	tunDevicePath    = "/dev/net/tun"
)

type machineDevice struct {
	Index      int
	Name       string
	IP         net.IP
	Mac        net.HardwareAddr
	GatewayMac net.HardwareAddr
}

type cubeDev struct {
	Index int
	Name  string
	IP    net.IP
	Mac   net.HardwareAddr
}

type tapDevice struct {
	Index          int
	Name           string
	IP             net.IP
	InUse          bool
	File           *os.File
	FilterAttached bool
	ARPInstalled   bool
	PortMappings   []PortMapping
	FailureCount   int
	LastError      string
	LastStage      string
}

func disableGRO(ifName string) error {
	const (
		SIOCETHTOOL  = 0x8946
		ETHTOOL_SGRO = 0x0000002c
	)

	type ethtoolValue struct {
		Cmd  uint32
		Data uint32
	}

	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM, 0)
	if err != nil {
		return fmt.Errorf("open socket for ethtool: %w", err)
	}
	defer unix.Close(fd)

	value := ethtoolValue{Cmd: ETHTOOL_SGRO, Data: 0}

	var ifr [40]byte
	copy(ifr[:], ifName)
	*(*uintptr)(unsafe.Pointer(&ifr[16])) = uintptr(unsafe.Pointer(&value))

	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), SIOCETHTOOL, uintptr(unsafe.Pointer(&ifr[0])))
	if errno != 0 {
		return fmt.Errorf("disable GRO on %s: %w", ifName, errno)
	}
	return nil
}

func getGatewayMacAddr(ifName string) (string, error) {
	link, err := netlinkLinkByName(ifName)
	if err != nil {
		return "", err
	}
	gatewayIP, err := defaultGatewayIP(link)
	if err != nil {
		return "", err
	}
	neighs, err := netlinkNeighList(link.Attrs().Index, netlink.FAMILY_V4)
	if err != nil {
		return "", err
	}
	for _, neigh := range neighs {
		if isUsableGatewayNeighbor(neigh, gatewayIP) {
			return neigh.HardwareAddr.String(), nil
		}
	}
	return "", fmt.Errorf("gateway mac for %s via %s not found", ifName, gatewayIP.String())
}

func defaultGatewayIP(link netlink.Link) (net.IP, error) {
	routes, err := netlinkRouteList(link, netlink.FAMILY_V4)
	if err != nil {
		return nil, err
	}
	var gatewayIP net.IP
	var gatewayMetric int
	for _, route := range routes {
		if !isIPv4DefaultRoute(route.Dst) || route.Gw.To4() == nil {
			continue
		}
		if gatewayIP == nil || route.Priority < gatewayMetric {
			gatewayIP = route.Gw.To4()
			gatewayMetric = route.Priority
		}
	}
	if gatewayIP == nil {
		return nil, fmt.Errorf("default gateway not found on %s", link.Attrs().Name)
	}
	return gatewayIP, nil
}

func isIPv4DefaultRoute(dst *net.IPNet) bool {
	if dst == nil {
		return true
	}
	ones, bits := dst.Mask.Size()
	return bits == 32 && ones == 0
}

func isUsableGatewayNeighbor(neigh netlink.Neigh, gatewayIP net.IP) bool {
	if neigh.Family != netlink.FAMILY_V4 || !neigh.IP.Equal(gatewayIP) || len(neigh.HardwareAddr) == 0 {
		return false
	}
	switch neigh.State {
	case unix.NUD_REACHABLE, unix.NUD_STALE, unix.NUD_DELAY, unix.NUD_PROBE, unix.NUD_PERMANENT:
		return true
	default:
		return false
	}
}

func getMachineDevice(ifName string) (*machineDevice, error) {
	link, err := netlinkLinkByName(ifName)
	if err != nil {
		return nil, err
	}
	addrs, err := netlink.AddrList(link, netlink.FAMILY_V4)
	if err != nil {
		return nil, err
	}
	if len(addrs) != 1 {
		return nil, fmt.Errorf("ipv4 address on %s is not unique", ifName)
	}
	gwMac, err := getGatewayMacAddr(ifName)
	if err != nil {
		return nil, err
	}
	gatewayMac, err := net.ParseMAC(gwMac)
	if err != nil {
		return nil, err
	}
	return &machineDevice{
		Index:      link.Attrs().Index,
		Name:       link.Attrs().Name,
		IP:         addrs[0].IP,
		Mac:        link.Attrs().HardwareAddr,
		GatewayMac: gatewayMac,
	}, nil
}

func getOrCreateCubeDev(ip net.IP, mask, mtu int, macAddr string) (*cubeDev, error) {
	desiredAddr := &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   ip,
			Mask: net.CIDRMask(mask, 32),
		},
	}
	link, err := netlinkLinkByName(cubeDevName)
	if err == nil {
		dummy, ok := link.(*netlink.Dummy)
		if !ok {
			return nil, fmt.Errorf("%s is not dummy", cubeDevName)
		}
		addrs, err := netlink.AddrList(dummy, netlink.FAMILY_V4)
		if err != nil {
			return nil, err
		}
		hasDesiredAddr := false
		for _, addr := range addrs {
			if addr.IPNet != nil && addr.IPNet.IP.Equal(ip) {
				ones, _ := addr.IPNet.Mask.Size()
				if ones == mask {
					hasDesiredAddr = true
					continue
				}
			}
			if err := netlink.AddrDel(dummy, &addr); err != nil {
				return nil, err
			}
		}
		if !hasDesiredAddr {
			if err := netlink.AddrAdd(dummy, desiredAddr); err != nil && !errors.Is(err, syscall.EEXIST) {
				return nil, err
			}
		}
		if dummy.Attrs().Flags&net.FlagUp == 0 {
			if err := netlink.LinkSetUp(dummy); err != nil {
				return nil, err
			}
		}
		if dummy.Attrs().MTU != mtu {
			if err := netlink.LinkSetMTU(dummy, mtu); err != nil {
				return nil, err
			}
		}
		return &cubeDev{
			Index: dummy.Index,
			Name:  cubeDevName,
			IP:    ip,
			Mac:   dummy.HardwareAddr,
		}, nil
	}
	gwAddr, err := net.ParseMAC(macAddr)
	if err != nil {
		return nil, err
	}
	dummy := &netlink.Dummy{
		LinkAttrs: netlink.LinkAttrs{
			Name:         cubeDevName,
			HardwareAddr: gwAddr,
			TxQLen:       txQLen,
		},
	}
	if err := netlink.LinkAdd(dummy); err != nil {
		return nil, err
	}
	if err := netlink.AddrAdd(dummy, desiredAddr); err != nil {
		return nil, err
	}
	if err := netlink.LinkSetUp(dummy); err != nil {
		return nil, err
	}
	if err := netlink.LinkSetMTU(dummy, mtu); err != nil {
		return nil, err
	}
	return &cubeDev{
		Index: dummy.Index,
		Name:  cubeDevName,
		IP:    ip,
		Mac:   dummy.HardwareAddr,
	}, nil
}

func addARPEntry(ip net.IP, mac string, cubeDevIndex int) error {
	macAddr, err := net.ParseMAC(mac)
	if err != nil {
		return err
	}
	return netlink.NeighSet(&netlink.Neigh{
		Family:       netlink.FAMILY_V4,
		IP:           ip,
		HardwareAddr: macAddr,
		LinkIndex:    cubeDevIndex,
		State:        unix.NUD_PERMANENT,
		Type:         unix.RTN_UNSPEC,
	})
}

func ensureRouteToCubeDev(cidr string, dev *cubeDev) error {
	if dev == nil || dev.Index == 0 {
		return fmt.Errorf("cube-dev is not initialized")
	}
	_, dst, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("parse mvm cidr %q: %w", cidr, err)
	}
	filter := &netlink.Route{
		LinkIndex: dev.Index,
		Dst:       dst,
		Scope:     netlink.SCOPE_LINK,
		Protocol:  unix.RTPROT_STATIC,
	}
	routes, err := netlinkRouteListFiltered(netlink.FAMILY_V4, filter, netlink.RT_FILTER_DST|netlink.RT_FILTER_OIF)
	if err != nil {
		return fmt.Errorf("list route for %s via %s: %w", dst.String(), dev.Name, err)
	}
	for _, route := range routes {
		if route.Dst != nil && route.Dst.String() == dst.String() && route.LinkIndex == dev.Index {
			return nil
		}
	}
	return netlinkRouteReplace(filter)
}

func newTap(ip net.IP, mvmMacAddr string, mtu, cubeDevIdx int) (_ *tapDevice, retErr error) {
	totalStart := time.Now()
	var linkAddDuration time.Duration
	var setVnetHdrDuration time.Duration
	var linkSetUpDuration time.Duration
	var attachFilterDuration time.Duration
	var setMTUDuration time.Duration
	var addARPDuration time.Duration
	logger := CubeLog.WithContext(context.Background())
	name := tapName(ip.String())
	defer func() {
		logger.Infof(
			"network-agent timing newTap: name=%s ip=%s link_add_ms=%.3f set_vnet_hdr_ms=%.3f link_set_up_ms=%.3f attach_filter_ms=%.3f set_mtu_ms=%.3f add_arp_ms=%.3f total_ms=%.3f err=%v",
			name,
			ip.String(),
			durationMillis(linkAddDuration),
			durationMillis(setVnetHdrDuration),
			durationMillis(linkSetUpDuration),
			durationMillis(attachFilterDuration),
			durationMillis(setMTUDuration),
			durationMillis(addARPDuration),
			durationMillis(time.Since(totalStart)),
			retErr,
		)
	}()
	tapConfig := &netlink.Tuntap{
		LinkAttrs: netlink.LinkAttrs{
			Name:  name,
			Flags: net.FlagUp,
		},
		Mode:   netlink.TUNTAP_MODE_TAP,
		Flags:  unix.IFF_TAP | unix.IFF_NO_PI | unix.IFF_VNET_HDR | unix.IFF_ONE_QUEUE,
		Queues: 1,
	}
	logger.Infof("network-agent newTap begin: name=%s ip=%s mtu=%d cube_dev_idx=%d flags=0x%x queues=%d",
		name, ip.String(), mtu, cubeDevIdx, tapConfig.Flags, tapConfig.Queues)
	stageStart := time.Now()
	if err := netlink.LinkAdd(tapConfig); err != nil {
		linkAddDuration = time.Since(stageStart)
		logger.Warnf("network-agent newTap link add failed: name=%s err=%v", name, err)
		return nil, err
	}
	linkAddDuration = time.Since(stageStart)
	defer func() {
		if retErr != nil {
			logger.Warnf("network-agent newTap cleanup after failure: name=%s ifindex=%d err=%v", name, tapConfig.Index, retErr)
			_ = destroyTap(tapConfig.Index)
		}
	}()
	tap := &tapDevice{
		IP:    ip,
		Name:  name,
		Index: tapConfig.Index,
		InUse: true,
	}
	if len(tapConfig.Fds) == 0 {
		logger.Warnf("network-agent newTap missing fd: name=%s ifindex=%d", tap.Name, tap.Index)
		return nil, fmt.Errorf("tap(%s) fd is empty", tap.Name)
	}
	tap.File = tapConfig.Fds[0]
	logger.Infof("network-agent newTap link add done: name=%s ifindex=%d fd=%d", tap.Name, tap.Index, tap.File.Fd())
	size := virtioNetHdrSize
	stageStart = time.Now()
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, tap.File.Fd(), uintptr(unix.TUNSETVNETHDRSZ), uintptr(unsafe.Pointer(&size))); errno != 0 {
		setVnetHdrDuration = time.Since(stageStart)
		logger.Warnf("network-agent newTap set vnet hdr failed: name=%s fd=%d size=%d errno=%v", tap.Name, tap.File.Fd(), size, errno)
		return nil, fmt.Errorf("set tap(%s) vnet hdr failed: %v", tap.Name, errno)
	}
	setVnetHdrDuration = time.Since(stageStart)
	logger.Infof("network-agent newTap set vnet hdr done: name=%s fd=%d size=%d", tap.Name, tap.File.Fd(), size)
	stageStart = time.Now()
	if err := netlink.LinkSetUp(tapConfig); err != nil {
		linkSetUpDuration = time.Since(stageStart)
		logger.Warnf("network-agent newTap link set up failed: name=%s ifindex=%d err=%v", tap.Name, tap.Index, err)
		return nil, err
	}
	linkSetUpDuration = time.Since(stageStart)
	logger.Infof("network-agent newTap link set up done: name=%s ifindex=%d", tap.Name, tap.Index)
	stageStart = time.Now()
	if err := cubevsAttachFilter(uint32(tap.Index)); err != nil {
		attachFilterDuration = time.Since(stageStart)
		logger.Warnf("network-agent newTap attach filter failed: name=%s ifindex=%d err=%v", tap.Name, tap.Index, err)
		return nil, err
	}
	attachFilterDuration = time.Since(stageStart)
	tap.FilterAttached = true
	logger.Infof("network-agent newTap attach filter done: name=%s ifindex=%d", tap.Name, tap.Index)
	stageStart = time.Now()
	if err := netlink.LinkSetMTU(tapConfig, mtu); err != nil {
		setMTUDuration = time.Since(stageStart)
		logger.Warnf("network-agent newTap set mtu failed: name=%s ifindex=%d mtu=%d err=%v", tap.Name, tap.Index, mtu, err)
		return nil, err
	}
	setMTUDuration = time.Since(stageStart)
	logger.Infof("network-agent newTap set mtu done: name=%s ifindex=%d mtu=%d", tap.Name, tap.Index, mtu)
	stageStart = time.Now()
	if err := addARPEntryFunc(ip, mvmMacAddr, cubeDevIdx); err != nil && err != syscall.EEXIST {
		addARPDuration = time.Since(stageStart)
		logger.Warnf("network-agent newTap add arp failed: name=%s ifindex=%d ip=%s mac=%s cube_dev_idx=%d err=%v",
			tap.Name, tap.Index, ip.String(), mvmMacAddr, cubeDevIdx, err)
		return nil, err
	}
	addARPDuration = time.Since(stageStart)
	tap.ARPInstalled = true
	logger.Infof("network-agent newTap ready: name=%s ifindex=%d ip=%s fd=%d arp_mac=%s",
		tap.Name, tap.Index, ip.String(), tap.File.Fd(), mvmMacAddr)
	return tap, nil
}

type ifReq struct {
	Name  [16]byte
	Flags uint16
}

func getTapFd(name string) (*os.File, error) {
	totalStart := time.Now()
	var linkByNameDuration time.Duration
	var openDuration time.Duration
	var tunSetIFFDuration time.Duration
	var setVnetHdrDuration time.Duration
	var resultErr error
	logger := CubeLog.WithContext(context.Background())
	defer func() {
		logger.Infof(
			"network-agent timing getTapFd: name=%s link_by_name_ms=%.3f open_tun_ms=%.3f tunsetiff_ms=%.3f set_vnet_hdr_ms=%.3f total_ms=%.3f err=%v",
			name,
			durationMillis(linkByNameDuration),
			durationMillis(openDuration),
			durationMillis(tunSetIFFDuration),
			durationMillis(setVnetHdrDuration),
			durationMillis(time.Since(totalStart)),
			resultErr,
		)
	}()
	stageStart := time.Now()
	link, err := netlinkLinkByName(name)
	linkByNameDuration = time.Since(stageStart)
	if err != nil {
		resultErr = err
		return nil, err
	}
	tap, ok := link.(*netlink.Tuntap)
	if !ok {
		resultErr = fmt.Errorf("%s is not tap", name)
		return nil, resultErr
	}

	stageStart = time.Now()
	fd, err := unixOpen(tunDevicePath, os.O_RDWR|syscall.O_CLOEXEC, 0)
	openDuration = time.Since(stageStart)
	if err != nil {
		resultErr = err
		return nil, err
	}

	var req ifReq
	copy(req.Name[:15], tap.Name)
	req.Flags = unix.IFF_TAP | unix.IFF_NO_PI | unix.IFF_VNET_HDR | unix.IFF_ONE_QUEUE

	stageStart = time.Now()
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.TUNSETIFF), uintptr(unsafe.Pointer(&req)))
	tunSetIFFDuration = time.Since(stageStart)
	if errno != 0 {
		unixClose(fd)
		resultErr = fmt.Errorf("set tap(%s) TUNSETIFF failed, errno: %+v", tap.Name, errno)
		return nil, resultErr
	}

	size := virtioNetHdrSize
	stageStart = time.Now()
	_, _, errno = unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.TUNSETVNETHDRSZ), uintptr(unsafe.Pointer(&size)))
	setVnetHdrDuration = time.Since(stageStart)
	if errno != 0 {
		unixClose(fd)
		resultErr = fmt.Errorf("set tap(%s) vnet hdr failed, errno: %+v", tap.Name, errno)
		return nil, resultErr
	}

	return os.NewFile(uintptr(fd), tunDevicePath), nil
}

func restoreTap(tap *tapDevice, mtu int, mvmMacAddr string, cubeDevIdx int) (*tapDevice, error) {
	totalStart := time.Now()
	var linkByNameDuration time.Duration
	var getTapFdDuration time.Duration
	var linkSetUpDuration time.Duration
	var setMTUDuration time.Duration
	var attachFilterDuration time.Duration
	attachFilterSkipped := false
	var addARPDuration time.Duration
	addARPSkipped := false
	var resultErr error
	nameForLog := ""
	ipForLog := ""
	if tap != nil {
		nameForLog = tap.Name
		if tap.IP != nil {
			ipForLog = tap.IP.String()
		}
	}
	logger := CubeLog.WithContext(context.Background())
	defer func() {
		logger.Infof(
			"network-agent timing restoreTap: name=%s ip=%s link_by_name_ms=%.3f get_tap_fd_ms=%.3f link_set_up_ms=%.3f set_mtu_ms=%.3f attach_filter_ms=%.3f attach_filter_skipped=%t add_arp_ms=%.3f add_arp_skipped=%t total_ms=%.3f err=%v",
			nameForLog,
			ipForLog,
			durationMillis(linkByNameDuration),
			durationMillis(getTapFdDuration),
			durationMillis(linkSetUpDuration),
			durationMillis(setMTUDuration),
			durationMillis(attachFilterDuration),
			attachFilterSkipped,
			durationMillis(addARPDuration),
			addARPSkipped,
			durationMillis(time.Since(totalStart)),
			resultErr,
		)
	}()
	if tap == nil {
		resultErr = fmt.Errorf("tap is nil")
		return nil, resultErr
	}
	if tap.IP == nil {
		resultErr = fmt.Errorf("tap %q missing ip", tap.Name)
		return nil, resultErr
	}
	name := tap.Name
	if name == "" {
		name = tapName(tap.IP.String())
	}
	nameForLog = name
	ipForLog = tap.IP.String()

	stageStart := time.Now()
	link, err := netlinkLinkByName(name)
	linkByNameDuration = time.Since(stageStart)
	if err != nil {
		resultErr = err
		return nil, err
	}
	sysTap, ok := link.(*netlink.Tuntap)
	if !ok {
		resultErr = fmt.Errorf("%s is not tap", name)
		return nil, resultErr
	}

	restored := &tapDevice{
		Name:           name,
		Index:          sysTap.Index,
		IP:             tap.IP.To4(),
		InUse:          link.Attrs().RawFlags&unix.IFF_LOWER_UP > 0,
		File:           tap.File,
		FilterAttached: tap.FilterAttached && tap.Index == sysTap.Index,
		ARPInstalled:   tap.ARPInstalled && tap.Index == sysTap.Index,
		PortMappings:   append([]PortMapping(nil), tap.PortMappings...),
	}

	if restored.File == nil {
		stageStart = time.Now()
		restored.File, err = getTapFd(name)
		getTapFdDuration = time.Since(stageStart)
		if err != nil {
			resultErr = err
			return nil, err
		}
	}

	if link.Attrs().Flags&net.FlagUp == 0 {
		stageStart = time.Now()
		if err := netlink.LinkSetUp(link); err != nil {
			linkSetUpDuration = time.Since(stageStart)
			resultErr = err
			return nil, err
		}
		linkSetUpDuration = time.Since(stageStart)
	}
	if sysTap.MTU != mtu {
		stageStart = time.Now()
		if err := netlink.LinkSetMTU(sysTap, mtu); err != nil {
			setMTUDuration = time.Since(stageStart)
			resultErr = err
			return nil, err
		}
		setMTUDuration = time.Since(stageStart)
	}
	if !restored.FilterAttached {
		stageStart = time.Now()
		if err := cubevsAttachFilter(uint32(restored.Index)); err != nil {
			attachFilterDuration = time.Since(stageStart)
			resultErr = err
			return nil, err
		}
		attachFilterDuration = time.Since(stageStart)
		restored.FilterAttached = true
	} else {
		attachFilterSkipped = true
	}
	if !restored.ARPInstalled {
		stageStart = time.Now()
		if err := addARPEntryFunc(restored.IP, mvmMacAddr, cubeDevIdx); err != nil && !errors.Is(err, syscall.EEXIST) {
			addARPDuration = time.Since(stageStart)
			resultErr = err
			return nil, err
		}
		addARPDuration = time.Since(stageStart)
		restored.ARPInstalled = true
	} else {
		addARPSkipped = true
	}
	return restored, nil
}

func listCubeTaps() (map[string]*tapDevice, error) {
	links, err := netlinkLinkList()
	if err != nil {
		return nil, err
	}
	ipToTap := make(map[string]*tapDevice)
	for _, link := range links {
		tap, ok := link.(*netlink.Tuntap)
		if !ok || tap.Mode != netlink.TUNTAP_MODE_TAP {
			continue
		}
		ipStr, err := extractIP(tap.Name)
		if err != nil {
			continue
		}
		ip := net.ParseIP(ipStr).To4()
		if ip == nil {
			continue
		}
		ipToTap[ip.String()] = &tapDevice{
			Name:  tap.Name,
			Index: tap.Index,
			IP:    ip,
			InUse: link.Attrs().RawFlags&unix.IFF_LOWER_UP > 0,
		}
	}
	return ipToTap, nil
}

func getTapByName(name string) (*tapDevice, error) {
	link, err := netlinkLinkByName(name)
	if err != nil {
		return nil, err
	}
	tap, ok := link.(*netlink.Tuntap)
	if !ok {
		return nil, fmt.Errorf("%s is not tap", name)
	}
	ipStr, err := extractIP(tap.Name)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(ipStr).To4()
	if ip == nil {
		return nil, fmt.Errorf("invalid tap ip for %s", name)
	}
	return &tapDevice{
		Name:  tap.Name,
		Index: tap.Index,
		IP:    ip,
		InUse: link.Attrs().RawFlags&unix.IFF_LOWER_UP > 0,
	}, nil
}

func destroyTap(ifIdx int) error {
	link, err := netlinkLinkByIndex(ifIdx)
	if err != nil {
		return err
	}
	if tap, ok := link.(*netlink.Tuntap); ok {
		if err := deletePersistentTapByName(tap.Name); err == nil {
			return nil
		}
	}
	return netlinkLinkDel(link)
}

func isTapMissingError(err error) bool {
	if err == nil {
		return false
	}
	var notFound netlink.LinkNotFoundError
	return errors.As(err, &notFound)
}

func deletePersistentTapByName(name string) error {
	req, err := unix.NewIfreq(name)
	if err != nil {
		return err
	}
	req.SetUint16(uint16(netlink.TUNTAP_MODE_TAP) | uint16(unix.IFF_TAP) | uint16(unix.IFF_NO_PI) | uint16(unix.IFF_VNET_HDR) | uint16(unix.IFF_ONE_QUEUE))
	fd, err := unixOpen(tunDevicePath, os.O_RDWR|syscall.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unixClose(fd)
	if err := unixIoctlIfreq(fd, unix.TUNSETIFF, req); err != nil {
		return err
	}
	if err := unixIoctlSetInt(fd, unix.TUNSETPERSIST, 0); err != nil {
		return err
	}
	return nil
}

func tapName(ip string) string {
	return tapNamePrefix + ip
}

func extractIP(name string) (string, error) {
	if len(name) <= len(tapNamePrefix) || name[:len(tapNamePrefix)] != tapNamePrefix {
		return "", fmt.Errorf("not cube tap: %s", name)
	}
	return name[len(tapNamePrefix):], nil
}
