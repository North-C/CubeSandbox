package cubevs

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"
	"unsafe"

	"github.com/cilium/ebpf"
	"golang.org/x/sys/unix"
)

const maxNetPolicyEntries = 8192

var alwaysDeniedSandboxCIDRs = []string{
	"10.0.0.0/8",
	"127.0.0.0/8",
	"169.254.0.0/16",
	"172.16.0.0/12",
	"192.168.0.0/16",
}

// newInnerLPMMap creates a new LPM trie map to be used as inner map
// for allow_out / deny_out hash-of-maps.
func newInnerLPMMap() (*ebpf.Map, error) {
	m, err := ebpf.NewMap(&ebpf.MapSpec{
		Type:       ebpf.LPMTrie,
		KeySize:    uint32(unsafe.Sizeof(lpmKey{})),
		ValueSize:  uint32(unsafe.Sizeof(uint32(0))),
		MaxEntries: maxNetPolicyEntries,
		Flags:      unix.BPF_F_NO_PREALLOC,
	})
	if err != nil {
		return nil, fmt.Errorf("ebpf.NewMap(LPMTrie) failed: %w", err)
	}
	return m, nil
}

// ensureInnerMap checks whether the outer hash-of-maps already has an
// inner map for the given ifindex.  If not, it creates one and inserts it.
func ensureInnerMap(outerMap *ebpf.Map, ifindex uint32, mapName string) (*ebpf.Map, error) {
	// Check if inner map already exists for this ifindex.
	var innerMapID uint32
	err := outerMap.Lookup(&ifindex, &innerMapID)
	if err == nil {
		inner, err := borrowMapFromID(ebpf.MapID(innerMapID))
		if err != nil {
			return nil, err
		}
		rememberInnerMap(mapName, ifindex, inner)
		return inner, nil
	}
	if !errors.Is(err, ebpf.ErrKeyNotExist) {
		return nil, fmt.Errorf("map.Lookup failed: %w, name: %s", err, mapName)
	}

	// Create a new inner LPM trie map and insert it.
	inner, err := newInnerLPMMap()
	if err != nil {
		return nil, err
	}

	err = outerMap.Put(&ifindex, inner)
	if err != nil {
		_ = inner.Close()
		return nil, fmt.Errorf("map.Put failed: %w, name: %s", err, mapName)
	}
	rememberInnerMap(mapName, ifindex, inner)
	return inner, nil
}

// initNetPolicy creates inner LPM trie maps for the given ifindex
// in both allow_out and deny_out hash-of-maps, if not already present.
// This should be called during AttachFilter.
func initNetPolicy(ifindex uint32) error {
	totalStart := time.Now()
	var ensureAllowDuration time.Duration
	var ensureDenyDuration time.Duration
	var resultErr error
	defer func() {
		emitTiming(
			"initNetPolicy",
			map[string]string{"ifindex": strconv.FormatUint(uint64(ifindex), 10)},
			map[string]time.Duration{
				"ensure_allow_out": ensureAllowDuration,
				"ensure_deny_out":  ensureDenyDuration,
				"total":            time.Since(totalStart),
			},
			resultErr,
		)
	}()

	allowOut, err := borrowPinnedMap(MapNameAllowOut)
	if err != nil {
		resultErr = err
		return resultErr
	}

	stageStart := time.Now()
	_, err = ensureInnerMap(allowOut, ifindex, MapNameAllowOut)
	ensureAllowDuration = time.Since(stageStart)
	if err != nil {
		resultErr = err
		return resultErr
	}

	denyOut, err := borrowPinnedMap(MapNameDenyOut)
	if err != nil {
		resultErr = err
		return resultErr
	}

	stageStart = time.Now()
	_, err = ensureInnerMap(denyOut, ifindex, MapNameDenyOut)
	ensureDenyDuration = time.Since(stageStart)
	if err != nil {
		resultErr = err
		return resultErr
	}
	return nil
}

// flushInnerMap removes all entries from the inner LPM trie map
// associated with the given ifindex in the outer hash-of-maps.
func flushInnerMap(outerMap *ebpf.Map, ifindex uint32) error {
	var innerMapID uint32
	err := outerMap.Lookup(&ifindex, &innerMapID)
	if err != nil {
		if errors.Is(err, ebpf.ErrKeyNotExist) {
			return nil
		}
		return fmt.Errorf("map.Lookup failed: %w", err)
	}

	inner, err := ebpf.NewMapFromID(ebpf.MapID(innerMapID))
	if err != nil {
		return fmt.Errorf("ebpf.NewMapFromID failed: %w, id: %d", err, innerMapID)
	}
	defer inner.Close()

	var key lpmKey
	iter := inner.Iterate()
	for iter.Next(&key, new(uint32)) {
		_ = inner.Delete(&key)
	}
	return nil
}

// cleanupNetPolicy flushes all entries in the inner LPM trie maps
// for the given ifindex in both allow_out and deny_out.
// This should be called during DelTAPDevice.
func cleanupNetPolicy(ifindex uint32) error {
	allowOut, err := loadPinnedMap(MapNameAllowOut)
	if err != nil {
		return err
	}
	defer allowOut.Close()

	err = flushInnerMap(allowOut, ifindex)
	if err != nil {
		return fmt.Errorf("flush %s failed: %w", MapNameAllowOut, err)
	}

	denyOut, err := loadPinnedMap(MapNameDenyOut)
	if err != nil {
		return err
	}
	defer denyOut.Close()

	return flushInnerMap(denyOut, ifindex)
}

// parseCIDR parses a CIDR string (e.g. "10.0.0.0/8") or a plain IP
// (e.g. "10.1.2.3") into an lpmKey.
func parseCIDR(s string) (lpmKey, error) {
	_, ipNet, err := net.ParseCIDR(s)
	if err != nil {
		// Try as a plain IP address (treated as /32).
		ip := net.ParseIP(s)
		if ip == nil {
			return lpmKey{}, fmt.Errorf("invalid CIDR or IP: %s", s) //nolint:err113
		}
		return lpmKey{Prefixlen: 32, IP: ipToUint32(ip)}, nil
	}
	ones, _ := ipNet.Mask.Size()
	return lpmKey{Prefixlen: uint32(ones), IP: ipToUint32(ipNet.IP)}, nil
}

// populateInnerMap parses the given CIDR list and inserts each entry
// into the inner LPM trie map for the specified ifindex.
func populateInnerMap(outerMap *ebpf.Map, mapName string, ifindex uint32, cidrs []string) error {
	totalStart := time.Now()
	var lookupDuration time.Duration
	var openInnerDuration time.Duration
	var updateDuration time.Duration
	var resultErr error
	defer func() {
		emitTiming(
			"populateInnerMap",
			map[string]string{
				"ifindex": strconv.FormatUint(uint64(ifindex), 10),
				"entries": strconv.Itoa(len(cidrs)),
				"map":     mapName,
			},
			map[string]time.Duration{
				"lookup_inner_id": lookupDuration,
				"open_inner_map":  openInnerDuration,
				"update_entries":  updateDuration,
				"total":           time.Since(totalStart),
			},
			resultErr,
		)
	}()

	inner := borrowedInnerMap(mapName, ifindex)
	if inner == nil {
		var innerMapID uint32
		stageStart := time.Now()
		err := outerMap.Lookup(&ifindex, &innerMapID)
		lookupDuration = time.Since(stageStart)
		if err != nil {
			resultErr = fmt.Errorf("map.Lookup failed: %w", err)
			return resultErr
		}

		stageStart = time.Now()
		inner, err = borrowMapFromID(ebpf.MapID(innerMapID))
		openInnerDuration = time.Since(stageStart)
		if err != nil {
			resultErr = err
			return resultErr
		}
		rememberInnerMap(mapName, ifindex, inner)
	}

	val := uint32(1)
	stageStart := time.Now()
	for _, cidr := range cidrs {
		key, err := parseCIDR(cidr)
		if err != nil {
			updateDuration += time.Since(stageStart)
			resultErr = err
			return resultErr
		}
		err = inner.Update(&key, &val, ebpf.UpdateAny)
		if err != nil {
			updateDuration += time.Since(stageStart)
			resultErr = fmt.Errorf("inner map update failed: %w, cidr: %s", err, cidr)
			return resultErr
		}
	}
	updateDuration += time.Since(stageStart)
	return nil
}

// applyNetPolicy configures egress network policy for the given ifindex
// based on MVMOptions.
//
// Rules:
//   - AllowOut non-empty: insert entries into allow_out inner map.
//   - DenyOut always includes alwaysDeniedSandboxCIDRs.
//   - AllowInternetAccess=false: DenyOut is set to "0.0.0.0/0" (deny all).
func applyNetPolicy(ifindex uint32, opts MVMOptions) error {
	totalStart := time.Now()
	var populateAllowDuration time.Duration
	var populateDenyDuration time.Duration
	var resultErr error
	allowCount := 0
	denyCount := 0
	defer func() {
		emitTiming(
			"applyNetPolicy",
			map[string]string{
				"ifindex":     strconv.FormatUint(uint64(ifindex), 10),
				"allow_count": strconv.Itoa(allowCount),
				"deny_count":  strconv.Itoa(denyCount),
			},
			map[string]time.Duration{
				"populate_allow": populateAllowDuration,
				"populate_deny":  populateDenyDuration,
				"total":          time.Since(totalStart),
			},
			resultErr,
		)
	}()

	// Process allowOut.
	var allowOut []string
	if opts.AllowOut != nil {
		allowOut = *opts.AllowOut
	}
	allowCount = len(allowOut)
	if len(allowOut) > 0 {
		allowOutMap, err := borrowPinnedMap(MapNameAllowOut)
		if err != nil {
			resultErr = err
			return resultErr
		}

		stageStart := time.Now()
		err = populateInnerMap(allowOutMap, MapNameAllowOut, ifindex, allowOut)
		populateAllowDuration = time.Since(stageStart)
		if err != nil {
			resultErr = fmt.Errorf("populate %s failed: %w", MapNameAllowOut, err)
			return resultErr
		}
	}

	// Process denyOut: always append alwaysDeniedSandboxCIDRs.
	// If AllowInternetAccess is false, deny all outbound traffic.
	var denyOut []string
	if opts.AllowInternetAccess != nil && !*opts.AllowInternetAccess {
		denyOut = []string{"0.0.0.0/0"}
	} else {
		if opts.DenyOut != nil {
			denyOut = append(*opts.DenyOut, alwaysDeniedSandboxCIDRs...)
		} else {
			denyOut = append(denyOut, alwaysDeniedSandboxCIDRs...)
		}
	}
	denyCount = len(denyOut)

	if len(denyOut) > 0 {
		denyOutMap, err := borrowPinnedMap(MapNameDenyOut)
		if err != nil {
			resultErr = err
			return resultErr
		}

		stageStart := time.Now()
		err = populateInnerMap(denyOutMap, MapNameDenyOut, ifindex, denyOut)
		populateDenyDuration = time.Since(stageStart)
		if err != nil {
			resultErr = fmt.Errorf("populate %s failed: %w", MapNameDenyOut, err)
			return resultErr
		}
	}

	return nil
}
