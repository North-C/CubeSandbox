// Copyright (c) 2024 Tencent Inc.
// SPDX-License-Identifier: Apache-2.0
//

package cube

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/tencentcloud/CubeSandbox/CubeMaster/api/services/cubebox/v1"
	"github.com/tencentcloud/CubeSandbox/CubeMaster/pkg/base/constants"
	"github.com/tencentcloud/CubeSandbox/CubeMaster/pkg/base/log"
	"github.com/tencentcloud/CubeSandbox/CubeMaster/pkg/errorcode"
	"github.com/tencentcloud/CubeSandbox/CubeMaster/pkg/service/httpservice/common"
	"github.com/tencentcloud/CubeSandbox/CubeMaster/pkg/service/sandbox"
	"github.com/tencentcloud/CubeSandbox/CubeMaster/pkg/service/sandbox/types"
	"github.com/tencentcloud/CubeSandbox/CubeMaster/pkg/templatecenter"
	"github.com/tencentcloud/CubeSandbox/cubelog"
)

var (
	createSandboxDealCubeboxCreateReqWithTemplateFn = dealCubeboxCreateReqWithTemplate
	createSandboxRunFn                              = sandbox.CreateSandbox
)

func createSandbox(w http.ResponseWriter, r *http.Request, rt *CubeLog.RequestTrace) interface{} {
	_ = w
	totalStart := time.Now()
	var constructReqDuration time.Duration
	var templateDuration time.Duration
	var affinityDuration time.Duration
	var createDuration time.Duration
	var requestID string
	var instanceType string
	var retCode int64 = -1
	defer func() {
		log.G(r.Context()).Infof(
			"cubemaster timing createSandbox http: request_id=%s instance_type=%s construct_req_ms=%.3f template_merge_ms=%.3f affinity_ms=%.3f create_sandbox_ms=%.3f total_ms=%.3f ret_code=%d",
			requestID,
			instanceType,
			durationMillis(constructReqDuration),
			durationMillis(templateDuration),
			durationMillis(affinityDuration),
			durationMillis(createDuration),
			durationMillis(time.Since(totalStart)),
			retCode,
		)
	}()
	rt.RetCode = -1
	rsp := &types.Res{
		Ret: &types.Ret{
			RetCode: -1,
			RetMsg:  http.StatusText(http.StatusNotFound),
		},
	}

	stageStart := time.Now()
	req, err := constructCreateReq(r)
	constructReqDuration = time.Since(stageStart)
	if err != nil {
		rsp.Ret.RetCode = int(errorcode.ErrorCode_MasterParamsError)
		rsp.Ret.RetMsg = err.Error()
		rt.RetCode = int64(errorcode.ErrorCode_MasterParamsError)
		retCode = rt.RetCode
		return rsp
	}
	requestID = req.RequestID
	instanceType = req.InstanceType
	rsp.RequestID = req.RequestID
	rt.RequestID = req.RequestID
	rt.InstanceType = req.InstanceType
	ctx := log.WithLogger(r.Context(), log.G(r.Context()).WithFields(map[string]any{
		"RequestId":    req.RequestID,
		"InstanceType": req.InstanceType,
	}))

	stageStart = time.Now()
	if err := createSandboxDealCubeboxCreateReqWithTemplateFn(ctx, req); err != nil {
		templateDuration = time.Since(stageStart)
		templateRetCode := errorcode.ErrorCode_MasterParamsError
		if errors.Is(err, templatecenter.ErrTemplateNotFound) {
			templateRetCode = errorcode.ErrorCode_NotFound
		}
		rsp.Ret.RetCode = int(templateRetCode)
		rsp.Ret.RetMsg = err.Error()
		rt.RetCode = int64(templateRetCode)
		retCode = rt.RetCode
		log.G(ctx).Error(err)
		return rsp
	}
	templateDuration = time.Since(stageStart)

	stageStart = time.Now()
	ctx = runInsReq2Affinity(ctx, req)
	affinityDuration = time.Since(stageStart)
	stageStart = time.Now()
	ret := createSandboxRunFn(ctx, req)
	createDuration = time.Since(stageStart)
	rt.RetCode = int64(ret.Ret.RetCode)
	retCode = rt.RetCode
	return ret
}

func constructCreateReq(r *http.Request) (*types.CreateCubeSandboxReq, error) {
	req := &types.CreateCubeSandboxReq{}
	if err := common.GetBodyReq(r, req); err != nil {
		return nil, err
	}

	if req.Request == nil {
		return nil, errors.New("requestID is nil")
	}

	if req.Labels == nil {
		req.Labels = map[string]string{}
	}
	if req.Annotations == nil {
		req.Annotations = map[string]string{}
	}
	constants.NormalizeAppSnapshotAnnotations(req.Annotations)
	if req.InstanceType == "" {
		if req.Annotations[constants.CubeAnnotationAppSnapshotTemplateID] != "" {
			req.InstanceType = cubebox.InstanceType_cubebox.String()
		} else {
			req.InstanceType = cubebox.InstanceType_cubebox.String()
		}
	}
	if req.NetworkType == "" {
		req.NetworkType = cubebox.NetworkType_tap.String()
	}
	if templateID := req.Annotations[constants.CubeAnnotationAppSnapshotTemplateID]; templateID != "" {
		req.Labels[constants.CubeAnnotationAppSnapshotTemplateID] = templateID
	}
	req.Labels[constants.Caller] = getCaller(r)
	req.Labels[constants.CubeAnnotationsInsType] = req.InstanceType
	if req.Namespace == "" {
		req.Namespace = "default"
	}
	return req, nil
}

func dealAppWhitelistHook(ctx context.Context, req *types.CreateCubeSandboxReq) {
	_ = ctx
	_ = req
}
