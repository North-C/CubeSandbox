// Copyright (c) 2024 Tencent Inc.
// SPDX-License-Identifier: Apache-2.0
//

package storage

import (
	"fmt"
	"time"

	"github.com/urfave/cli/v2"
)

var Command = &cli.Command{
	Name:    "storage",
	Aliases: []string{"s"},
	Usage:   "Manage storage",
	Subcommands: []*cli.Command{
		lsdb,
		cleanup,
	},
}

func myPrint(format string, a ...interface{}) {
	fmt.Printf("%v,"+format+"\n",
		append([]interface{}{fmt.Sprintf("%v", time.Now().Format(time.RFC3339Nano))}, a...)...)
}
