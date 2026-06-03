// Copyright (c) 2024 Tencent Inc.
// SPDX-License-Identifier: Apache-2.0
//

package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tencentcloud/CubeSandbox/CubeNet/cubevs"
	CubeLog "github.com/tencentcloud/CubeSandbox/cubelog"
)

func durationMillis(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000
}

func installCubeVSTimingHook() {
	cubevs.SetTimingHook(func(name string, fields map[string]string, durations map[string]time.Duration, err error) {
		CubeLog.WithContext(context.Background()).Infof(
			"network-agent timing cubevs.%s: %s %s err=%v",
			name,
			formatTimingFields(fields),
			formatTimingDurations(durations),
			err,
		)
	})
}

func formatTimingFields(fields map[string]string) string {
	if len(fields) == 0 {
		return ""
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", key, fields[key]))
	}
	return strings.Join(parts, " ")
}

func formatTimingDurations(durations map[string]time.Duration) string {
	if len(durations) == 0 {
		return ""
	}
	keys := make([]string, 0, len(durations))
	for key := range durations {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s_ms=%.3f", key, durationMillis(durations[key])))
	}
	return strings.Join(parts, " ")
}
