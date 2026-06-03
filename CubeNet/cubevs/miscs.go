package cubevs

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/rlimit"
)

func init() {
	_ = rlimit.RemoveMemlock()
}

func rewriteConstants(vars map[string]*ebpf.VariableSpec, params Params) error {
	var err error
	err = errors.Join(err, vars[globalNameMVMInnerIP].Set(ipToUint32(params.MVMInnerIP)))
	err = errors.Join(err, vars[globalNameMVMMacaddrP1].Set(hardwareAddrToUint32(params.MVMMacAddr)))
	err = errors.Join(err, vars[globalNameMVMMacaddrP2].Set(hardwareAddrToUint16(params.MVMMacAddr)))
	err = errors.Join(err, vars[globalNameMVMGatewayIP].Set(ipToUint32(params.MVMGatewayIP)))
	err = errors.Join(err, vars[globalNameCubegw0IP].Set(ipToUint32(params.Cubegw0IP)))
	err = errors.Join(err, vars[globalNameCubegw0Ifindex].Set(params.Cubegw0Ifindex))
	err = errors.Join(err, vars[globalNameCubegw0MacaddrP1].Set(hardwareAddrToUint32(params.Cubegw0MacAddr)))
	err = errors.Join(err, vars[globalNameCubegw0MacaddrP2].Set(hardwareAddrToUint16(params.Cubegw0MacAddr)))
	err = errors.Join(err, vars[globalNameNodeIP].Set(ipToUint32(params.NodeIP)))
	err = errors.Join(err, vars[globalNameNodeIfindex].Set(params.NodeIfindex))
	err = errors.Join(err, vars[globalNameNodeMacaddrP1].Set(hardwareAddrToUint32(params.NodeMacAddr)))
	err = errors.Join(err, vars[globalNameNodeMacaddrP2].Set(hardwareAddrToUint16(params.NodeMacAddr)))
	err = errors.Join(err, vars[globalNameNodeGatewayMacaddrP1].Set(hardwareAddrToUint32(params.NodeGatewayMacAddr)))
	err = errors.Join(err, vars[globalNameNodeGatewayMacaddrP2].Set(hardwareAddrToUint16(params.NodeGatewayMacAddr)))
	return err
}

func pinProgs(obj *ebpf.Collection) error {
	for progName, prog := range obj.Programs {
		pinnedPath := pinPath(progName)
		_ = os.Remove(pinnedPath) // NOCC:Path Traversal()
		err := prog.Pin(pinnedPath)
		if err != nil {
			return fmt.Errorf("ebpf.Program.Pin failed: %w, name: %s", err, progName)
		}
	}
	return nil
}

func loadObject(params Params, loader func() (*ebpf.CollectionSpec, error), name string) error {
	opts := ebpf.CollectionOptions{
		Maps: ebpf.MapOptions{
			PinPath: bpfFSPath,
		},
	}

	spec, err := loader()
	if err != nil {
		return fmt.Errorf("%s failed: %w", name, err)
	}

	err = rewriteConstants(spec.Variables, params)
	if err != nil {
		return fmt.Errorf("%s rewriteConstants failed: %w", name, err)
	}

	obj, err := ebpf.NewCollectionWithOptions(spec, opts)
	if err != nil {
		return fmt.Errorf("ebpf.NewCollectionWithOptions: %w", err)
	}
	defer obj.Close()

	return pinProgs(obj)
}

func attachTCFilter(progName string, ifindex uint32, direction TCDirection) error {
	prog, err := ebpf.LoadPinnedProgram(pinPath(progName), nil)
	if err != nil {
		return fmt.Errorf("ebpf.LoadPinnedProgram failed: %w, name: %s", err, progName)
	}
	defer prog.Close()

	err = createQdisc(ifindex)
	if err != nil {
		return err
	}

	err = attachFilter(ifindex, uint32(prog.FD()), progName, direction)
	if err != nil {
		return err
	}
	return nil
}

// Init should be called once before invoking any other CubeVS APIs.
func Init(params Params) error {
	resetPinnedObjectCache()

	_ = os.Remove(pinPath("tungrp_to_tuns")) // NOCC:Path Traversal()

	err := loadObject(params, loadLocalgw, "loadLocalgw")
	if err != nil {
		return err
	}

	err = loadObject(params, loadMvmtap, "loadMvmtap")
	if err != nil {
		return err
	}

	err = loadObject(params, loadNodenic, "loadNodenic")
	if err != nil {
		return err
	}

	// attach TC filter to cube-dev
	err = attachTCFilter(programNameFromEnvoy, params.Cubegw0Ifindex, TCEgress)
	if err != nil {
		return err
	}

	// attach TC filter to eth0
	err = attachTCFilter(programNameFromWorld, params.NodeIfindex, TCIngress)
	if err != nil {
		return err
	}

	// attach TC filter to lo
	err = attachTCFilter(programNameFromWorld, 1, TCIngress)
	if err != nil {
		return err
	}

	resetPinnedObjectCache()
	return nil
}

// AttachFilter attaches a BPF TC filter to the ingress path of the TAP device specified by ifindex.
func AttachFilter(ifindex uint32) error {
	totalStart := time.Now()
	var borrowProgramDuration time.Duration
	var createQdiscDuration time.Duration
	var attachFilterDuration time.Duration
	var initNetPolicyDuration time.Duration
	var resultErr error
	defer func() {
		emitTiming(
			"AttachFilter",
			map[string]string{"ifindex": strconv.FormatUint(uint64(ifindex), 10)},
			map[string]time.Duration{
				"borrow_program":  borrowProgramDuration,
				"create_qdisc":    createQdiscDuration,
				"attach_filter":   attachFilterDuration,
				"init_net_policy": initNetPolicyDuration,
				"total":           time.Since(totalStart),
			},
			resultErr,
		)
	}()

	stageStart := time.Now()
	prog, err := borrowPinnedProgram(programNameFromCube)
	borrowProgramDuration = time.Since(stageStart)
	if err != nil {
		resultErr = err
		return resultErr
	}

	stageStart = time.Now()
	err = createQdisc(ifindex)
	createQdiscDuration = time.Since(stageStart)
	if err != nil {
		resultErr = err
		return resultErr
	}

	stageStart = time.Now()
	err = attachFilter(ifindex, uint32(prog.FD()), programNameFromCube, TCIngress)
	attachFilterDuration = time.Since(stageStart)
	if err != nil {
		resultErr = err
		return resultErr
	}

	stageStart = time.Now()
	err = initNetPolicy(ifindex)
	initNetPolicyDuration = time.Since(stageStart)
	if err != nil {
		resultErr = err
		return resultErr
	}
	return nil
}
