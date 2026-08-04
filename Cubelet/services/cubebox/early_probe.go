// Copyright (c) 2026 Tencent Inc.
// SPDX-License-Identifier: Apache-2.0

package cubebox

import (
	"os"
	"strconv"
	"time"
)

const (
	earlyProbeEnv        = "CUBESANDBOX_EARLY_PROBE"
	earlyProbeDelayMsEnv = "CUBESANDBOX_EARLY_PROBE_DELAY_MS"
)

func earlyProbeEnabled() bool {
	return os.Getenv(earlyProbeEnv) == "1"
}

func earlyProbeDelay() time.Duration {
	delayMs, err := strconv.Atoi(os.Getenv(earlyProbeDelayMsEnv))
	if err != nil || delayMs <= 0 {
		return 0
	}
	return time.Duration(delayMs) * time.Millisecond
}
