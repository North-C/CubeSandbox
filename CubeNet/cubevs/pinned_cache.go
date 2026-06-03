package cubevs

import (
	"fmt"
	"sync"

	"github.com/cilium/ebpf"
)

var (
	ebpfLoadPinnedMap     = ebpf.LoadPinnedMap
	ebpfLoadPinnedProgram = ebpf.LoadPinnedProgram

	pinnedObjects = &pinnedObjectCache{
		maps:      make(map[string]*ebpf.Map),
		mapsByID:  make(map[ebpf.MapID]*ebpf.Map),
		innerMaps: make(map[string]*ebpf.Map),
		programs:  make(map[string]*ebpf.Program),
	}
)

type pinnedObjectCache struct {
	mu        sync.RWMutex
	maps      map[string]*ebpf.Map
	mapsByID  map[ebpf.MapID]*ebpf.Map
	innerMaps map[string]*ebpf.Map
	programs  map[string]*ebpf.Program
}

func borrowPinnedMap(name string) (*ebpf.Map, error) {
	return pinnedObjects.borrowMap(name)
}

func borrowPinnedProgram(name string) (*ebpf.Program, error) {
	return pinnedObjects.borrowProgram(name)
}

func borrowMapFromID(id ebpf.MapID) (*ebpf.Map, error) {
	return pinnedObjects.borrowMapFromID(id)
}

func borrowedInnerMap(mapName string, ifindex uint32) *ebpf.Map {
	return pinnedObjects.innerMap(mapName, ifindex)
}

func rememberInnerMap(mapName string, ifindex uint32, m *ebpf.Map) {
	pinnedObjects.rememberInnerMap(mapName, ifindex, m)
}

func resetPinnedObjectCache() {
	pinnedObjects.reset()
}

func innerMapKey(mapName string, ifindex uint32) string {
	return fmt.Sprintf("%s:%d", mapName, ifindex)
}

func (c *pinnedObjectCache) borrowMap(name string) (*ebpf.Map, error) {
	c.mu.RLock()
	m := c.maps[name]
	c.mu.RUnlock()
	if m != nil {
		return m, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	m = c.maps[name]
	if m != nil {
		return m, nil
	}

	m, err := ebpfLoadPinnedMap(pinPath(name), nil)
	if err != nil {
		return nil, fmt.Errorf("ebpf.LoadPinnedMap failed: %w, name: %s", err, name)
	}
	c.maps[name] = m
	return m, nil
}

func (c *pinnedObjectCache) borrowMapFromID(id ebpf.MapID) (*ebpf.Map, error) {
	c.mu.RLock()
	m := c.mapsByID[id]
	c.mu.RUnlock()
	if m != nil {
		return m, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	m = c.mapsByID[id]
	if m != nil {
		return m, nil
	}

	m, err := ebpf.NewMapFromID(id)
	if err != nil {
		return nil, fmt.Errorf("ebpf.NewMapFromID failed: %w, id: %d", err, id)
	}
	c.mapsByID[id] = m
	return m, nil
}

func (c *pinnedObjectCache) innerMap(mapName string, ifindex uint32) *ebpf.Map {
	c.mu.RLock()
	m := c.innerMaps[innerMapKey(mapName, ifindex)]
	c.mu.RUnlock()
	return m
}

func (c *pinnedObjectCache) rememberInnerMap(mapName string, ifindex uint32, m *ebpf.Map) {
	if m == nil {
		return
	}
	c.mu.Lock()
	c.innerMaps[innerMapKey(mapName, ifindex)] = m
	c.mu.Unlock()
}

func (c *pinnedObjectCache) borrowProgram(name string) (*ebpf.Program, error) {
	c.mu.RLock()
	prog := c.programs[name]
	c.mu.RUnlock()
	if prog != nil {
		return prog, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	prog = c.programs[name]
	if prog != nil {
		return prog, nil
	}

	prog, err := ebpfLoadPinnedProgram(pinPath(name), nil)
	if err != nil {
		return nil, fmt.Errorf("ebpf.LoadPinnedProgram failed: %w, name: %s", err, name)
	}
	c.programs[name] = prog
	return prog, nil
}

func (c *pinnedObjectCache) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	closedMaps := make(map[*ebpf.Map]struct{}, len(c.maps)+len(c.mapsByID)+len(c.innerMaps))
	closeMap := func(m *ebpf.Map) {
		if m == nil {
			return
		}
		if _, ok := closedMaps[m]; ok {
			return
		}
		closedMaps[m] = struct{}{}
		_ = m.Close()
	}
	for _, m := range c.maps {
		closeMap(m)
	}
	for _, m := range c.mapsByID {
		closeMap(m)
	}
	for _, m := range c.innerMaps {
		closeMap(m)
	}
	for _, prog := range c.programs {
		_ = prog.Close()
	}

	c.maps = make(map[string]*ebpf.Map)
	c.mapsByID = make(map[ebpf.MapID]*ebpf.Map)
	c.innerMaps = make(map[string]*ebpf.Map)
	c.programs = make(map[string]*ebpf.Program)
}
