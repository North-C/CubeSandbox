// Copyright (c) 2024 Tencent Inc.
// SPDX-License-Identifier: Apache-2.0
//

//go:build integration || mock

package mockdebug

import "github.com/tencentcloud/CubeSandbox/CubeMaster/integration"

func Init(enabled bool) error {
	if enabled {
		integration.MockInit()
	}
	return nil
}
