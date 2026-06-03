package cubevs

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/cilium/ebpf"
)

// AddPortMapping adds port mapping for host port and guest port.
func AddPortMapping(ifindex uint32, listenPort uint16, hostPort uint16) error {
	totalStart := time.Now()
	origListenPort := listenPort
	origHostPort := hostPort
	var updateRemoteDuration time.Duration
	var updateLocalDuration time.Duration
	var resultErr error
	defer func() {
		emitTiming(
			"AddPortMapping",
			map[string]string{
				"ifindex":     strconv.FormatUint(uint64(ifindex), 10),
				"listen_port": strconv.Itoa(int(origListenPort)),
				"host_port":   strconv.Itoa(int(origHostPort)),
			},
			map[string]time.Duration{
				"update_remote": updateRemoteDuration,
				"update_local":  updateLocalDuration,
				"total":         time.Since(totalStart),
			},
			resultErr,
		)
	}()

	listenPort = htons(listenPort)
	hostPort = htons(hostPort)
	mvmPort := MVMPort{
		Ifindex:    ifindex,
		ListenPort: listenPort,
	}

	// host port => ifindex + listen port
	stageStart := time.Now()
	m1, err := borrowPinnedMap(MapNameRemotePortMapping)
	if err != nil {
		updateRemoteDuration = time.Since(stageStart)
		resultErr = err
		return resultErr
	}

	err = m1.Update(&hostPort, &mvmPort, ebpf.UpdateAny)
	updateRemoteDuration = time.Since(stageStart)
	if err != nil {
		resultErr = fmt.Errorf("map.Update failed: %w, name: %s", err, MapNameRemotePortMapping)
		return resultErr
	}

	// ifindex + listen port => host port
	stageStart = time.Now()
	m2, err := borrowPinnedMap(MapNameLocalPortMapping)
	if err != nil {
		updateLocalDuration = time.Since(stageStart)
		resultErr = err
		return resultErr
	}

	err = m2.Update(&mvmPort, &hostPort, ebpf.UpdateAny)
	updateLocalDuration = time.Since(stageStart)
	if err != nil {
		resultErr = fmt.Errorf("map.Update failed: %w, name: %s", err, MapNameLocalPortMapping)
		return resultErr
	}

	return nil
}

// DelPortMapping deletes existing port mapping from CubeVS.
// This will cause network interruption and should be called only when the MVM exits.
// Note: This ignores `ebpf.ErrKeyNotExist` (if called multiple times) to ensure deletion.
func DelPortMapping(ifindex uint32, listenPort uint16, hostPort uint16) error {
	m1, err := loadPinnedMap(MapNameLocalPortMapping)
	if err != nil {
		return err
	}
	defer m1.Close()

	mvmPort := MVMPort{
		Ifindex:    ifindex,
		ListenPort: htons(listenPort),
	}
	err = m1.Delete(&mvmPort)
	if err != nil && !errors.Is(err, ebpf.ErrKeyNotExist) {
		return fmt.Errorf("map.Delete failed: %w, name: %s", err, MapNameLocalPortMapping)
	}

	m2, err := loadPinnedMap(MapNameRemotePortMapping)
	if err != nil {
		return err
	}
	defer m2.Close()

	hostPort = htons(hostPort)
	err = m2.Delete(&hostPort)
	if err != nil && !errors.Is(err, ebpf.ErrKeyNotExist) {
		return fmt.Errorf("map.Delete failed: %w, name: %s", err, MapNameRemotePortMapping)
	}

	return nil
}

// ListPortMapping returns host port to guest port (with ifindex) mapping.
func ListPortMapping() (map[uint16]MVMPort, error) {
	m1, err := loadPinnedMap(MapNameRemotePortMapping)
	if err != nil {
		return nil, err
	}
	defer m1.Close()

	var (
		key    uint16
		value  MVMPort
		result = make(map[uint16]MVMPort)
	)

	iter := m1.Iterate()
	for iter.Next(&key, &value) {
		result[ntohs(key)] = MVMPort{
			Ifindex:    value.Ifindex,
			ListenPort: ntohs(value.ListenPort),
		}
	}
	err = iter.Err()
	if err != nil {
		return nil, fmt.Errorf("map.Iterate failed: %w, name: %s", err, MapNameRemotePortMapping)
	}

	m2, err := loadPinnedMap(MapNameLocalPortMapping)
	if err != nil {
		return nil, err
	}
	defer m2.Close()

	iter = m2.Iterate()
	for iter.Next(&value, &key) {
		result[ntohs(key)] = MVMPort{
			Ifindex:    value.Ifindex,
			ListenPort: ntohs(value.ListenPort),
		}
	}
	err = iter.Err()
	if err != nil {
		return nil, fmt.Errorf("map.Iterate failed: %w, name: %s", err, MapNameLocalPortMapping)
	}

	return result, nil
}

// GetPortMapping returns the host port assigned for the specified ifindex and listen port.
func GetPortMapping(ifindex uint32, listenPort uint16) (uint16, error) {
	m, err := loadPinnedMap(MapNameLocalPortMapping)
	if err != nil {
		return 0, err
	}
	defer m.Close()

	var hostPort uint16
	err = m.Lookup(&MVMPort{
		Ifindex:    ifindex,
		ListenPort: htons(listenPort),
	}, &hostPort)
	if err != nil {
		return 0, fmt.Errorf("map.Lookup failed: %w, name: %s", err, MapNameLocalPortMapping)
	}

	return ntohs(hostPort), nil
}
