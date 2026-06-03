// Copyright (c) 2024 Tencent Inc.
// SPDX-License-Identifier: Apache-2.0
//

// Package cubelet provides the interface for the cube-master to interact with the cube-node.
package cubelet

import (
	"context"
	"strconv"
	"strings"
	"time"

	cubebox "github.com/tencentcloud/CubeSandbox/CubeMaster/api/services/cubebox/v1"
	imagesv1 "github.com/tencentcloud/CubeSandbox/CubeMaster/api/services/images/v1"
	"github.com/tencentcloud/CubeSandbox/CubeMaster/pkg/base/config"
	"github.com/tencentcloud/CubeSandbox/CubeMaster/pkg/base/log"
	"github.com/tencentcloud/CubeSandbox/CubeMaster/pkg/base/ret"
	"github.com/tencentcloud/CubeSandbox/CubeMaster/pkg/cubelet/grpcconn"
	"github.com/tencentcloud/CubeSandbox/CubeMaster/pkg/errorcode"
)

func Destroy(ctx context.Context, calleeEp string,
	req *cubebox.DestroyCubeSandboxRequest) (*cubebox.DestroyCubeSandboxResponse, error) {
	conn, err := grpcconn.GetWorkerConn(ctx, calleeEp)
	if err != nil {
		return nil, ret.Err(errorcode.ErrorCode_ConnHostFailed, err.Error())
	}
	defer conn.Close()
	c := cubebox.NewCubeboxMgrClient(conn.Value())
	ctx, cancel := context.WithTimeout(context.Background(),
		time.Duration(config.GetConfig().CubeletConf.CommonTimeoutInsec)*time.Second)
	defer cancel()
	return c.Destroy(ctx, req)
}

func Create(ctx context.Context, calleeEp string,
	req *cubebox.RunCubeSandboxRequest) (*cubebox.RunCubeSandboxResponse, error) {
	totalStart := time.Now()
	var getConnDuration time.Duration
	var rpcDuration time.Duration
	var resultErr error
	retCode := int32(-1)
	defer func() {
		log.G(ctx).Infof(
			"cubemaster timing cubelet.Create rpc: request_id=%s endpoint=%s get_conn_ms=%.3f grpc_create_ms=%.3f total_ms=%.3f ret_code=%d err=%v",
			req.GetRequestID(),
			calleeEp,
			durationMillis(getConnDuration),
			durationMillis(rpcDuration),
			durationMillis(time.Since(totalStart)),
			retCode,
			resultErr,
		)
	}()
	stageStart := time.Now()
	conn, err := grpcconn.GetWorkerConn(ctx, calleeEp)
	getConnDuration = time.Since(stageStart)
	if err != nil {
		resultErr = ret.Err(errorcode.ErrorCode_ConnHostFailed, err.Error())
		return nil, resultErr
	}
	defer conn.Close()
	c := cubebox.NewCubeboxMgrClient(conn.Value())

	stageStart = time.Now()
	rsp, err := c.Create(ctx, req)
	rpcDuration = time.Since(stageStart)
	if rsp != nil && rsp.GetRet() != nil {
		retCode = int32(rsp.GetRet().GetRetCode())
	}
	resultErr = err
	return rsp, err
}

func AppSnapshot(ctx context.Context, calleeEp string,
	req *cubebox.AppSnapshotRequest) (*cubebox.AppSnapshotResponse, error) {
	conn, err := grpcconn.GetWorkerConn(ctx, calleeEp)
	if err != nil {
		return nil, ret.Err(errorcode.ErrorCode_ConnHostFailed, err.Error())
	}
	defer conn.Close()
	c := cubebox.NewCubeboxMgrClient(conn.Value())
	return c.AppSnapshot(ctx, req)
}

func CommitSandbox(ctx context.Context, calleeEp string,
	req *cubebox.CommitSandboxRequest) (*cubebox.CommitSandboxResponse, error) {
	conn, err := grpcconn.GetWorkerConn(ctx, calleeEp)
	if err != nil {
		return nil, ret.Err(errorcode.ErrorCode_ConnHostFailed, err.Error())
	}
	defer conn.Close()
	c := cubebox.NewCubeboxMgrClient(conn.Value())
	return c.CommitSandbox(ctx, req)
}

func CleanupTemplate(ctx context.Context, calleeEp string,
	req *cubebox.CleanupTemplateRequest) (*cubebox.CleanupTemplateResponse, error) {
	conn, err := grpcconn.GetWorkerConn(ctx, calleeEp)
	if err != nil {
		return nil, ret.Err(errorcode.ErrorCode_ConnHostFailed, err.Error())
	}
	defer conn.Close()
	c := cubebox.NewCubeboxMgrClient(conn.Value())
	return c.CleanupTemplate(ctx, req)
}

func List(ctx context.Context, calleeEp string,
	req *cubebox.ListCubeSandboxRequest) (*cubebox.ListCubeSandboxResponse, error) {
	conn, err := grpcconn.GetWorkerConn(ctx, calleeEp)
	if err != nil {
		return nil, ret.Err(errorcode.ErrorCode_ConnHostFailed, err.Error())
	}
	defer conn.Close()
	c := cubebox.NewCubeboxMgrClient(conn.Value())
	ctx, cancel := context.WithTimeout(context.Background(),
		time.Duration(config.GetConfig().CubeletConf.CommonTimeoutInsec)*time.Second)
	defer cancel()
	return c.List(ctx, req)
}

func CreateImage(ctx context.Context, calleeEp string,
	req *imagesv1.CreateImageRequest) (*imagesv1.CreateImageRequestResponse, error) {
	conn, err := grpcconn.GetWorkerConn(ctx, calleeEp)
	if err != nil {
		return nil, ret.Err(errorcode.ErrorCode_ConnHostFailed, err.Error())
	}
	defer conn.Close()
	c := imagesv1.NewImagesClient(conn.Value())
	ctx, cancel := context.WithTimeout(context.Background(),
		time.Duration(config.GetConfig().CubeletConf.CreateImageTimeoutInSec)*time.Second)
	defer cancel()

	return c.CreateImage(ctx, req)
}

func DeleteImage(ctx context.Context, calleeEp string, req *imagesv1.DestroyImageRequest) (*imagesv1.DestroyImageResponse,
	error) {
	conn, err := grpcconn.GetWorkerConn(ctx, calleeEp)
	if err != nil {
		return nil, ret.Err(errorcode.ErrorCode_ConnHostFailed, err.Error())
	}
	defer conn.Close()
	c := imagesv1.NewImagesClient(conn.Value())
	ctx, cancel := context.WithTimeout(context.Background(),
		time.Duration(config.GetConfig().CubeletConf.CreateImageTimeoutInSec)*time.Second)
	defer cancel()
	return c.DestroyImage(ctx, req)
}

func GetCubeletAddr(hostIP string) string {
	return strings.Join([]string{hostIP,
		strconv.Itoa(config.GetConfig().CubeletConf.Grpc.GrpcPort)}, ":")
}

func Update(ctx context.Context, calleeEp string,
	req *cubebox.UpdateCubeSandboxRequest) (*cubebox.UpdateCubeSandboxResponse, error) {
	conn, err := grpcconn.GetWorkerConn(ctx, calleeEp)
	if err != nil {
		return nil, ret.Err(errorcode.ErrorCode_ConnHostFailed, err.Error())
	}
	defer conn.Close()
	c := cubebox.NewCubeboxMgrClient(conn.Value())
	ctx, cancel := context.WithTimeout(context.Background(),
		time.Duration(config.GetConfig().CubeletConf.CommonTimeoutInsec)*time.Second)
	defer cancel()
	return c.Update(ctx, req)
}

func Exec(ctx context.Context, calleeEp string,
	req *cubebox.ExecCubeSandboxRequest) (*cubebox.ExecCubeSandboxResponse, error) {
	conn, err := grpcconn.GetWorkerConn(ctx, calleeEp)
	if err != nil {
		return nil, ret.Err(errorcode.ErrorCode_ConnHostFailed, err.Error())
	}
	defer conn.Close()
	c := cubebox.NewCubeboxMgrClient(conn.Value())
	ctx, cancel := context.WithTimeout(context.Background(),
		time.Duration(config.GetConfig().CubeletConf.CommonTimeoutInsec)*time.Second)
	defer cancel()
	return c.Exec(ctx, req)
}
