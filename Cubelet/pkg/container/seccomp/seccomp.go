// Copyright (c) 2024 Tencent Inc.
// SPDX-License-Identifier: Apache-2.0
//

package seccomp

import (
	"context"

	"github.com/tencentcloud/CubeSandbox/Cubelet/api/services/cubebox/v1"

	"github.com/containerd/containerd/v2/core/containers"
	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/opencontainers/runtime-spec/specs-go"
)

func GenOpt(ctx context.Context, reqSysCalls []*cubebox.SysCall) oci.SpecOpts {
	var sysCalls []specs.LinuxSyscall
	for _, v := range reqSysCalls {
		var args []specs.LinuxSeccompArg
		if v.GetArgs() != nil {
			for _, vv := range v.GetArgs() {
				args = append(args, specs.LinuxSeccompArg{
					Index:    uint(vv.Index),
					Value:    vv.Value,
					ValueTwo: vv.ValueTwo,
					Op:       specs.LinuxSeccompOperator(vv.Op),
				})
			}
		}
		var errno *uint = nil
		if v.Errno != 0 {
			eTmp := uint(v.Errno)
			errno = &eTmp
		}
		sysCalls = append(sysCalls, specs.LinuxSyscall{
			Names:    v.Names,
			Action:   specs.LinuxSeccompAction(v.Action),
			ErrnoRet: errno,
			Args:     args,
		})
	}
	return withSeccomp(sysCalls)
}

func withSeccomp(sysCalls []specs.LinuxSyscall) oci.SpecOpts {
	return func(ctx context.Context, client oci.Client, c *containers.Container, s *specs.Spec) error {
		if s.Linux == nil {
			s.Linux = &specs.Linux{}
		}
		if s.Linux.Seccomp == nil {
			s.Linux.Seccomp = &specs.LinuxSeccomp{}
		}

		s.Linux.Seccomp.Syscalls = append(s.Linux.Seccomp.Syscalls, sysCalls...)
		return nil
	}
}
