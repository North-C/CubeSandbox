// Copyright (c) 2026 Tencent Inc.
// SPDX-License-Identifier: Apache-2.0

package cubebox

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEarlyProbeEnabled(t *testing.T) {
	t.Setenv(earlyProbeEnv, "1")
	require.True(t, earlyProbeEnabled())

	t.Setenv(earlyProbeEnv, "0")
	require.False(t, earlyProbeEnabled())

	t.Setenv(earlyProbeEnv, "")
	require.False(t, earlyProbeEnabled())
}

func TestEarlyProbeDelay(t *testing.T) {
	t.Setenv(earlyProbeDelayMsEnv, "50")
	require.Equal(t, 50*time.Millisecond, earlyProbeDelay())

	t.Setenv(earlyProbeDelayMsEnv, "0")
	require.Zero(t, earlyProbeDelay())

	t.Setenv(earlyProbeDelayMsEnv, "-1")
	require.Zero(t, earlyProbeDelay())

	t.Setenv(earlyProbeDelayMsEnv, "invalid")
	require.Zero(t, earlyProbeDelay())
}
