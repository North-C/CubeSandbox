package cubevs

import "time"

type TimingHook func(name string, fields map[string]string, durations map[string]time.Duration, err error)

var cubeVSTimingHook TimingHook

// SetTimingHook installs a process-local hook for CubeVS timing diagnostics.
// Passing nil disables timing emission.
func SetTimingHook(hook TimingHook) {
	cubeVSTimingHook = hook
}

func emitTiming(name string, fields map[string]string, durations map[string]time.Duration, err error) {
	hook := cubeVSTimingHook
	if hook != nil {
		hook(name, fields, durations, err)
	}
}
