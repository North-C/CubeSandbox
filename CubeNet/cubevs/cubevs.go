// Package cubevs is a library to manage CubeVS.
package cubevs

import (
	"errors"
	"net"
	"unsafe"

	"github.com/florianl/go-tc"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64 localgw ../src/localgw.bpf.c -- -I../vmlinux/x86
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64 mvmtap  ../src/mvmtap.bpf.c  -- -I../vmlinux/x86
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64 nodenic ../src/nodenic.bpf.c -- -I../vmlinux/x86
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target arm64 localgw ../src/localgw.bpf.c -- -I../vmlinux/arm64
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target arm64 mvmtap  ../src/mvmtap.bpf.c  -- -I../vmlinux/arm64
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target arm64 nodenic ../src/nodenic.bpf.c -- -I../vmlinux/arm64

// Params is used to initialize CubeVS.
type Params struct {
	// IP and MAC address inside MVMs
	MVMInnerIP net.IP
	MVMMacAddr net.HardwareAddr
	// Gateway IP for MVMs
	MVMGatewayIP net.IP
	// Ifindex, IP and MAC address of the cubegw0 device (a.k.a cubedev)
	Cubegw0Ifindex uint32
	Cubegw0IP      net.IP
	Cubegw0MacAddr net.HardwareAddr
	// Ifindex, IP and MAC address of Node itself
	NodeIfindex uint32
	NodeIP      net.IP
	NodeMacAddr net.HardwareAddr
	// MAC address of the Node gateway (next hop)
	NodeGatewayMacAddr net.HardwareAddr
}

// TAPDevice contains info about a TAP device.
type TAPDevice struct {
	IP      net.IP
	ID      string
	Ifindex int
}

// mvmMetadata is used to retrieve BPF map values.
// The struct layout should be exactly the same as BPF side.
type mvmMetadata struct {
	Version  uint32
	IP       uint32
	UUID     [64]byte
	Reserved [56]uint8
}

// TCDirection is used to specified attach point of a TC filter.
type TCDirection uint32

const (
	// TCIngress attaches TC filter to the ingress path.
	TCIngress = TCDirection(tc.HandleMinIngress)
	// TCEgress attaches TC filter to the egress path.
	TCEgress = TCDirection(tc.HandleMinEgress)
)

// MVMPort is used to store and retrieve port mapping.
// The struct layout should be exactly the same as BPF side.
type MVMPort struct {
	Ifindex    uint32
	ListenPort uint16
	Reserved   uint16
}

type lpmKey struct {
	Prefixlen uint32
	IP        uint32
}

const (
	// max length of MVM ID.
	maxIDLength = 64
	// programs that power CubeVS.
	programNameFromEnvoy = "from_envoy"
	programNameFromCube  = "from_cube"
	programNameFromWorld = "from_world"
	// MapNameIfindexToMVMMetadata and the following are maps created by CubeVS.
	MapNameIfindexToMVMMetadata = "ifindex_to_mvmmeta"
	MapNameMVMIPToIfindex       = "mvmip_to_ifindex"
	MapNameRemotePortMapping    = "remote_port_mapping"
	MapNameLocalPortMapping     = "local_port_mapping"
	MapNameAllowOut             = "allow_out"
	MapNameDenyOut              = "deny_out"
	// constants referenced by BPF programs.
	globalNameMVMInnerIP           = "mvm_inner_ip"
	globalNameMVMMacaddrP1         = "mvm_macaddr_p1"
	globalNameMVMMacaddrP2         = "mvm_macaddr_p2"
	globalNameMVMGatewayIP         = "mvm_gateway_ip"
	globalNameCubegw0IP            = "cubegw0_ip"
	globalNameCubegw0Ifindex       = "cubegw0_ifindex"
	globalNameCubegw0MacaddrP1     = "cubegw0_macaddr_p1"
	globalNameCubegw0MacaddrP2     = "cubegw0_macaddr_p2"
	globalNameNodeIP               = "nodenic_ip"
	globalNameNodeIfindex          = "nodenic_ifindex"
	globalNameNodeMacaddrP1        = "nodenic_macaddr_p1"
	globalNameNodeMacaddrP2        = "nodenic_macaddr_p2"
	globalNameNodeGatewayMacaddrP1 = "nodegw_macaddr_p1"
	globalNameNodeGatewayMacaddrP2 = "nodegw_macaddr_p2"
	// for bpffs.
	bpfFSPath = "/sys/fs/bpf"
	// for TC.
	tcFlagDirectAction        = 1
	tcFilterHandle            = 1
	tcFilterPriority          = 1
	tcHandleClsact            = tc.HandleIngress
	tcHandleMajMask    uint32 = 0xFFFF0000
	tcHandleMinMask    uint32 = 0x0000FFFF
	tcAttrKindBPF             = "bpf"
	tcAttrKindClsact          = "clsact"
)

// Errors that will be returned to upper layer.
var (
	// ErrProgNotExist is returned when there is no specified BPF program in BPF object.
	ErrProgNotExist = errors.New("BPF program not exists")
	// ErrTooLong is returned when the provided MVM ID is too long.
	ErrTooLong = errors.New("MVM ID is too long")
)

func _() {
	{
		// static assert, make sure MVMIdentity is of size 128
		var arr [128]struct{}
		var obj mvmMetadata
		const size = unsafe.Sizeof(obj)
		_ = arr[size-1]   // error if size > 128
		_ = arr[size-128] // error if size < 128
	}

	{
		// static assert, make sure MVMPort is of size 8
		var arr [8]struct{}
		var obj MVMPort
		const size = unsafe.Sizeof(obj)
		_ = arr[size-1] // error if size > 8
		_ = arr[size-8] // error if size < 8
	}

	{
		// static assert, make sure snatIP is of size 16
		var arr [16]struct{}
		var obj snatIP
		const size = unsafe.Sizeof(obj)
		_ = arr[size-1]  // error if size > 16
		_ = arr[size-16] // error if size < 16
	}

	{
		// static assert, make sure SessionKey is of size 20
		var arr [20]struct{}
		var obj sessionKey
		const size = unsafe.Sizeof(obj)
		_ = arr[size-1]  // error if size > 20
		_ = arr[size-20] // error if size < 20
	}

	{
		// static assert, make sure NATSession is of size 64
		var arr [64]struct{}
		var obj natSession
		const size = unsafe.Sizeof(obj)
		_ = arr[size-1]  // error if size > 64
		_ = arr[size-64] // error if size < 64
	}

	{
		// static assert, make sure IngressSession is of size 16
		var arr [16]struct{}
		var obj ingressSessionValue
		const size = unsafe.Sizeof(obj)
		_ = arr[size-1]  // error if size > 16
		_ = arr[size-16] // error if size < 16
	}

	{
		// static assert, make sure LpmKey is of size 8
		var arr [8]struct{}
		var obj lpmKey
		const size = unsafe.Sizeof(obj)
		_ = arr[size-1] // error if size > 8
		_ = arr[size-8] // error if size < 8
	}
}
