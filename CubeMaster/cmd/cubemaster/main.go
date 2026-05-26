// Copyright (c) 2024 Tencent Inc.
// SPDX-License-Identifier: Apache-2.0
//

package main

import (
	"flag"
	stdlog "log"
	"runtime/debug"

	"github.com/tencentcloud/CubeSandbox/CubeMaster/cmd/cubemaster/app"
	"github.com/tencentcloud/CubeSandbox/CubeMaster/cmd/cubemaster/mockdebug"
	"github.com/tencentcloud/CubeSandbox/CubeMaster/pkg/base/config"
	"github.com/tencentcloud/CubeSandbox/CubeMaster/pkg/base/recov"
	"github.com/tencentcloud/CubeSandbox/CubeMaster/pkg/base/version"
	_ "go.uber.org/automaxprocs"
)

var versionFlag = flag.Bool("v", false, "show version")

func main() {
	flag.Parse()
	if *versionFlag {
		version.ShowAndExit(true)
	}

	debug.SetGCPercent(90)
	app := app.New()

	cfg, err := config.Init()
	if err != nil {
		stdlog.Fatalf("config init fail:%v", recov.DumpStacktrace(3, err))
		return
	}

	if err := mockdebug.Init(cfg.Common.MockDebug); err != nil {
		stdlog.Fatalf("mock debug init fail:%v", recov.DumpStacktrace(3, err))
		return
	}
	app.Run()
}
