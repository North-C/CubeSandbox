// Copyright (c) 2024 Tencent Inc.
// SPDX-License-Identifier: Apache-2.0
//

package seccomp

import (
	"context"
	"testing"

	"github.com/tencentcloud/CubeSandbox/Cubelet/api/services/cubebox/v1"

	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
)

func TestGenOpt(t *testing.T) {
	in := []*cubebox.SysCall{
		{
			Names:  []string{"ptrace"},
			Action: string(specs.ActAllow),
		},
	}
	specFunc := GenOpt(context.Background(), in)
	if specFunc == nil {
		assert.FailNow(t, "should not nil")
	}
	s := oci.Spec{}
	err := specFunc(context.Background(), nil, nil, &s)
	assert.Nil(t, err)
	assert.NotNil(t, s.Linux)
	syscalls := []specs.LinuxSyscall{
		{
			Names:  []string{"ptrace"},
			Action: specs.ActAllow,
		},
	}
	assert.Equal(t, syscalls, s.Linux.Seccomp.Syscalls)
}
