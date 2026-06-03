package cubevs

import (
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/cilium/ebpf"
)

type MVMOptions struct {
	AllowInternetAccess *bool
	AllowOut            *[]string // CIDR or IP
	DenyOut             *[]string // CIDR or IP
}

// ListTAPDevices lists all TAP devices that managed by CubeVS.
func ListTAPDevices() ([]TAPDevice, error) {
	m, err := loadPinnedMap(MapNameIfindexToMVMMetadata)
	if err != nil {
		return nil, err
	}
	defer m.Close()

	var taps []TAPDevice
	var key uint32
	var value mvmMetadata
	iter := m.Iterate()
	for iter.Next(&key, &value) {
		taps = append(taps, TAPDevice{
			IP:      uint32ToIP(value.IP),
			ID:      bytesToString(value.UUID[:]),
			Ifindex: int(key),
		})
	}
	err = iter.Err()
	if err != nil {
		return nil, fmt.Errorf("map.Iterate failed: %w, name: %s", err, MapNameIfindexToMVMMetadata)
	}

	return taps, nil
}

// AddTAPDevice adds a new device to CubeVS.
func AddTAPDevice(ifindex uint32, ip net.IP, id string, version uint32, opts MVMOptions) error {
	totalStart := time.Now()
	var idPrepareDuration time.Duration
	var updateMetadataDuration time.Duration
	var updateIPIndexDuration time.Duration
	var applyNetPolicyDuration time.Duration
	var resultErr error
	defer func() {
		emitTiming(
			"AddTAPDevice",
			map[string]string{
				"ifindex": strconv.FormatUint(uint64(ifindex), 10),
				"ip":      ip.String(),
				"id":      id,
			},
			map[string]time.Duration{
				"prepare":             idPrepareDuration,
				"update_mvm_metadata": updateMetadataDuration,
				"update_mvm_ip_index": updateIPIndexDuration,
				"apply_net_policy":    applyNetPolicyDuration,
				"total":               time.Since(totalStart),
			},
			resultErr,
		)
	}()

	stageStart := time.Now()
	if len(id) > maxIDLength {
		idPrepareDuration = time.Since(stageStart)
		resultErr = ErrTooLong
		return resultErr
	}

	mvmIP := ipToUint32(ip)
	mvmID := mvmMetadata{
		IP:      mvmIP,
		UUID:    stringToByteArray(id),
		Version: version,
	}
	idPrepareDuration = time.Since(stageStart)

	// ifindex <-> MVM metadata (IP, ID and tunnels)
	stageStart = time.Now()
	m, err := borrowPinnedMap(MapNameIfindexToMVMMetadata)
	if err != nil {
		updateMetadataDuration = time.Since(stageStart)
		resultErr = err
		return resultErr
	}

	err = m.Update(&ifindex, &mvmID, ebpf.UpdateAny)
	updateMetadataDuration = time.Since(stageStart)
	if err != nil {
		resultErr = fmt.Errorf("map.Update failed: %w, name: %s", err, MapNameIfindexToMVMMetadata)
		return resultErr
	}

	// MVM IP <-> ifindex
	stageStart = time.Now()
	m, err = borrowPinnedMap(MapNameMVMIPToIfindex)
	if err != nil {
		updateIPIndexDuration = time.Since(stageStart)
		resultErr = err
		return resultErr
	}

	err = m.Update(&mvmIP, &ifindex, ebpf.UpdateAny)
	updateIPIndexDuration = time.Since(stageStart)
	if err != nil {
		resultErr = fmt.Errorf("map.Update failed: %w, name: %s", err, MapNameMVMIPToIfindex)
		return resultErr
	}

	stageStart = time.Now()
	err = applyNetPolicy(ifindex, opts)
	applyNetPolicyDuration = time.Since(stageStart)
	if err != nil {
		resultErr = err
		return resultErr
	}
	return nil
}

// DelTAPDevice removes a TAP device from CubeVS.
func DelTAPDevice(ifindex uint32, ip net.IP) error {
	// Clean up network policy inner map entries first.
	err := cleanupNetPolicy(ifindex)
	if err != nil {
		return err
	}

	mvmIP := ipToUint32(ip)

	// ifindex <-> MVM metadata
	m, err := loadPinnedMap(MapNameIfindexToMVMMetadata)
	if err != nil {
		return err
	}
	defer m.Close()

	err = m.Delete(&ifindex)
	if err != nil {
		return fmt.Errorf("map.Delete failed: %w, name: %s", err, MapNameIfindexToMVMMetadata)
	}

	// MVM IP <-> ifindex
	m, err = loadPinnedMap(MapNameMVMIPToIfindex)
	if err != nil {
		return err
	}
	defer m.Close()

	err = m.Delete(&mvmIP)
	if err != nil {
		return fmt.Errorf("map.Delete failed: %w, name: %s", err, MapNameMVMIPToIfindex)
	}

	return nil
}

// GetTAPDevice returns a TAP device associated with the specific ifindex.
func GetTAPDevice(ifindex uint32) (*TAPDevice, error) {
	m, err := loadPinnedMap(MapNameIfindexToMVMMetadata)
	if err != nil {
		return nil, err
	}
	defer m.Close()

	var mvmMeta mvmMetadata
	err = m.Lookup(&ifindex, &mvmMeta)
	if err != nil {
		return nil, fmt.Errorf("map.Lookup failed: %w, name: %s", err, MapNameIfindexToMVMMetadata)
	}

	return &TAPDevice{
		IP:      uint32ToIP(mvmMeta.IP),
		ID:      bytesToString(mvmMeta.UUID[:]),
		Ifindex: int(ifindex),
	}, nil
}
