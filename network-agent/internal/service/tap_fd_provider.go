// Copyright (c) 2024 Tencent Inc.
// SPDX-License-Identifier: Apache-2.0
//

package service

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"

	CubeLog "github.com/tencentcloud/CubeSandbox/cubelog"
)

// TapFDProvider exposes the original TAP fd owned by network-agent.
type TapFDProvider interface {
	GetTapFile(sandboxID, tapName string) (*os.File, error)
}

func (s *localService) GetTapFile(sandboxID, tapName string) (*os.File, error) {
	totalStart := time.Now()
	lockStart := time.Now()
	s.mu.Lock()
	lockWait := time.Since(lockStart)
	lockAcquired := time.Now()
	var restoreDuration time.Duration
	restored := false
	var resultErr error
	var resultFile *os.File
	defer func() {
		lockHold := time.Since(lockAcquired)
		s.mu.Unlock()
		fd := uintptr(0)
		if resultFile != nil {
			fd = resultFile.Fd()
		}
		CubeLog.WithContext(context.Background()).Infof(
			"network-agent timing GetTapFile: sandbox_id=%s tap_name=%s restored=%t fd=%d lock_wait_ms=%.3f lock_hold_ms=%.3f restore_tap_ms=%.3f total_ms=%.3f err=%v",
			sandboxID,
			tapName,
			restored,
			fd,
			durationMillis(lockWait),
			durationMillis(lockHold),
			durationMillis(restoreDuration),
			durationMillis(time.Since(totalStart)),
			resultErr,
		)
	}()

	state, ok := s.states[sandboxID]
	if !ok {
		resultErr = fmt.Errorf("sandbox %q not found", sandboxID)
		return nil, resultErr
	}
	if tapName != "" && state.TapName != tapName {
		resultErr = fmt.Errorf("tap name mismatch: want %q got %q", tapName, state.TapName)
		return nil, resultErr
	}
	if state.tap == nil || state.tap.File == nil {
		restored = true
		baseTap := state.tap
		if baseTap == nil {
			baseTap = &tapDevice{
				Name:         state.TapName,
				IP:           net.ParseIP(state.SandboxIP).To4(),
				PortMappings: append([]PortMapping(nil), state.PortMappings...),
			}
		} else {
			baseTap.PortMappings = append([]PortMapping(nil), state.PortMappings...)
		}
		restoreStart := time.Now()
		tap, err := restoreTapFunc(baseTap, s.cfg.MvmMtu, s.cfg.MVMMacAddr, s.cubeDev.Index)
		restoreDuration = time.Since(restoreStart)
		if err != nil {
			resultErr = fmt.Errorf("tap fd unavailable for sandbox %q: %w", sandboxID, err)
			return nil, resultErr
		}
		state.tap = tap
	}
	resultFile = state.tap.File
	return resultFile, nil
}
