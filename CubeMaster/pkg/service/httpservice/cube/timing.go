// Copyright (c) 2024 Tencent Inc.
// SPDX-License-Identifier: Apache-2.0
//

package cube

import "time"

func durationMillis(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000.0
}
