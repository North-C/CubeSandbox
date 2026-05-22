// Copyright (c) 2024 Tencent Inc.
// SPDX-License-Identifier: Apache-2.0
//

package unsafe

import (
	"fmt"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/tencentcloud/CubeSandbox/Cubelet/cmd/cubecli/commands/cubebox"
)

const cmdTimeout = 3 * time.Second

var Command = &cli.Command{
	Name:    "unsafe",
	Aliases: []string{"u"},
	Usage:   "unsafe operations",
	Subcommands: []*cli.Command{
		Init,
		RestoreDB,
		DestroyTap,
		RemoveImage,
		cubebox.Destroy,
		cubebox.DestroyAll,
		volumedb,
	},
}

func myPrint(format string, a ...interface{}) {
	fmt.Printf("%v,"+format+"\n",
		append([]interface{}{fmt.Sprintf("%v", time.Now().Format(time.RFC3339Nano))}, a...)...)
}
