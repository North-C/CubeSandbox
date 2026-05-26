// Copyright (c) 2024 Tencent Inc.
// SPDX-License-Identifier: Apache-2.0
//

//go:build !integration && !mock

package mockdebug

import "errors"

// Init rejects mock debug mode in production builds. The real mock
// implementation is only compiled with the integration or mock build tag.
func Init(enabled bool) error {
	if !enabled {
		return nil
	}
	return errors.New("mock_debug requires building cubemaster with the integration or mock build tag")
}
