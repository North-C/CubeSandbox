// Copyright (c) 2024 Tencent Inc.
// SPDX-License-Identifier: Apache-2.0
//

package volume

import (
	"fmt"
	"github.com/urfave/cli/v2"
	"time"
)

var Command = &cli.Command{
	Name:    "volume",
	Aliases: []string{"v"},
	Usage:   "manage volumes",
	Subcommands: []*cli.Command{
		resetvolumeref,
		resetVolumeRefExec,
	},
}

func myPrint(format string, a ...interface{}) {
	fmt.Printf("%v,"+format+"\n",
		append([]interface{}{fmt.Sprintf("%v", time.Now().Format(time.RFC3339Nano))}, a...)...)
}
