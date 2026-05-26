// Copyright (c) 2024 Tencent Inc.
// SPDX-License-Identifier: Apache-2.0
//

package cubebox

import (
	jsoniter "github.com/json-iterator/go"
	"github.com/smallnest/weighted"
	"github.com/tencentcloud/CubeSandbox/CubeMaster/pkg/service/sandbox/types"
)

var mockScheduleReq = `{
	"requestID":"3548c646-212e-45cc-8eeb-f9b6b7c84fe5",
	"timeout":30,
	"containers":[
		{
			"name":"runtime-sidecar",
			"image":{
				"image":"busybox:latest"
			},
			"command":[
				"/data/sidecar/runtime-sidecar"
			],
			"resources":{
				"cpu":"100m",
				"mem":"64Mi"
			}
		}
	],
	"annotations":{
		"com.cube.debug":"true",
		"com.invoke_port":"8080",
		"com.netid":"gw-axgkcimt"
	}
}`

type resourceFormat struct {
	Weight int
	Res    *types.Resource
}

type loadBalancer struct {
	SW *weighted.SW
}

func newLoadBalancer(servers []*resourceFormat) *loadBalancer {
	sw := weighted.SW{}
	for _, s := range servers {
		sw.Add(s.Res, s.Weight)
	}
	return &loadBalancer{SW: &sw}
}

func (lb *loadBalancer) GetCreateCubeSandboxReq() *types.CreateCubeSandboxReq {
	item := lb.SW.Next()
	res, ok := item.(*types.Resource)
	if !ok {
		return nil
	}
	reqC := &types.CreateCubeSandboxReq{}
	_ = jsoniter.Unmarshal([]byte(mockScheduleReq), reqC)
	reqC.Containers[0].Resources = res
	return reqC
}

func getAllFormatList() map[string][]*resourceFormat {
	return map[string][]*resourceFormat{
		"all": {
			{Weight: 7, Res: &types.Resource{Cpu: "100m", Mem: "64Mi"}},
			{Weight: 23, Res: &types.Resource{Cpu: "100m", Mem: "128Mi"}},
			{Weight: 30, Res: &types.Resource{Cpu: "200m", Mem: "256Mi"}},
			{Weight: 1, Res: &types.Resource{Cpu: "300m", Mem: "384Mi"}},
			{Weight: 8, Res: &types.Resource{Cpu: "400m", Mem: "512Mi"}},
			{Weight: 1, Res: &types.Resource{Cpu: "600m", Mem: "768Mi"}},
			{Weight: 1, Res: &types.Resource{Cpu: "700m", Mem: "896Mi"}},
			{Weight: 7, Res: &types.Resource{Cpu: "800m", Mem: "1024Mi"}},
			{Weight: 3, Res: &types.Resource{Cpu: "1100m", Mem: "1408Mi"}},
			{Weight: 6, Res: &types.Resource{Cpu: "1600m", Mem: "2048Mi"}},
			{Weight: 6, Res: &types.Resource{Cpu: "2000m", Mem: "3072Mi"}},
			{Weight: 1, Res: &types.Resource{Cpu: "4000m", Mem: "6144Mi"}},
		},
		"bj": {
			{Weight: 24, Res: &types.Resource{Cpu: "100m", Mem: "64Mi"}},
			{Weight: 38, Res: &types.Resource{Cpu: "100m", Mem: "128Mi"}},
			{Weight: 20, Res: &types.Resource{Cpu: "200m", Mem: "256Mi"}},
			{Weight: 1, Res: &types.Resource{Cpu: "300m", Mem: "384Mi"}},
			{Weight: 3, Res: &types.Resource{Cpu: "400m", Mem: "512Mi"}},
			{Weight: 0, Res: &types.Resource{Cpu: "600m", Mem: "768Mi"}},
			{Weight: 0, Res: &types.Resource{Cpu: "700m", Mem: "896Mi"}},
			{Weight: 3, Res: &types.Resource{Cpu: "800m", Mem: "1024Mi"}},
			{Weight: 0, Res: &types.Resource{Cpu: "1100m", Mem: "1408Mi"}},
			{Weight: 7, Res: &types.Resource{Cpu: "1600m", Mem: "2048Mi"}},
			{Weight: 4, Res: &types.Resource{Cpu: "2000m", Mem: "3072Mi"}},
			{Weight: 0, Res: &types.Resource{Cpu: "4000m", Mem: "6144Mi"}},
		},
		"sh": {
			{Weight: 2, Res: &types.Resource{Cpu: "100m", Mem: "64Mi"}},
			{Weight: 36, Res: &types.Resource{Cpu: "100m", Mem: "128Mi"}},
			{Weight: 4, Res: &types.Resource{Cpu: "200m", Mem: "256Mi"}},
			{Weight: 3, Res: &types.Resource{Cpu: "300m", Mem: "384Mi"}},
			{Weight: 7, Res: &types.Resource{Cpu: "400m", Mem: "512Mi"}},
			{Weight: 4, Res: &types.Resource{Cpu: "500m", Mem: "640Mi"}},
			{Weight: 7, Res: &types.Resource{Cpu: "600m", Mem: "768Mi"}},
			{Weight: 7, Res: &types.Resource{Cpu: "700m", Mem: "896Mi"}},
			{Weight: 11, Res: &types.Resource{Cpu: "800m", Mem: "1024Mi"}},
			{Weight: 2, Res: &types.Resource{Cpu: "1100m", Mem: "1408Mi"}},
			{Weight: 5, Res: &types.Resource{Cpu: "1600m", Mem: "2048Mi"}},
			{Weight: 5, Res: &types.Resource{Cpu: "2000m", Mem: "3072Mi"}},
			{Weight: 4, Res: &types.Resource{Cpu: "4000m", Mem: "6144Mi"}},
			{Weight: 1, Res: &types.Resource{Cpu: "4000m", Mem: "14336Mi"}},
		},
		"gz": {
			{Weight: 11, Res: &types.Resource{Cpu: "100m", Mem: "64Mi"}},
			{Weight: 49, Res: &types.Resource{Cpu: "100m", Mem: "128Mi"}},
			{Weight: 9, Res: &types.Resource{Cpu: "200m", Mem: "256Mi"}},
			{Weight: 0, Res: &types.Resource{Cpu: "300m", Mem: "384Mi"}},
			{Weight: 8, Res: &types.Resource{Cpu: "400m", Mem: "512Mi"}},
			{Weight: 0, Res: &types.Resource{Cpu: "600m", Mem: "768Mi"}},
			{Weight: 4, Res: &types.Resource{Cpu: "700m", Mem: "896Mi"}},
			{Weight: 4, Res: &types.Resource{Cpu: "800m", Mem: "1024Mi"}},
			{Weight: 0, Res: &types.Resource{Cpu: "1100m", Mem: "1408Mi"}},
			{Weight: 2, Res: &types.Resource{Cpu: "1600m", Mem: "2048Mi"}},
			{Weight: 9, Res: &types.Resource{Cpu: "2000m", Mem: "3072Mi"}},
			{Weight: 2, Res: &types.Resource{Cpu: "4000m", Mem: "6144Mi"}},
		},
	}
}
