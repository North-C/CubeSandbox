package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/tencentcloud/CubeSandbox/CubeNet/cubevs"
)

const (
	cubeGW     = "cubegw0"
	cubeGWPeer = "gwpeer"
	nodeNIC    = "node0"
	nodePeer   = "nodepeer"
	tapDev     = "tap0"
	tapPeer    = "tappeer"

	mvmIP      = "10.0.0.2"
	mvmInnerIP = "169.254.68.6"
	mvmGWIP    = "169.254.68.5"
	cubeGWIP   = "192.0.2.1"
	nodeIP     = "198.51.100.2"
	nodePeerIP = "198.51.100.1"
	externalIP = "203.0.113.10"

	listenPort = 8080
	hostPort   = 18080
)

func main() {
	if err := runSmoke(); err != nil {
		fmt.Fprintf(os.Stderr, "\nFAIL: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("\nPASS: CubeVS TC attach and traffic smoke test completed")
}

func runSmoke() error {
	if os.Geteuid() != 0 {
		return errors.New("must run as root")
	}
	for _, name := range []string{"ip", "tc", "ping", "mountpoint", "mount"} {
		if _, err := exec.LookPath(name); err != nil {
			return fmt.Errorf("required command %q not found: %w", name, err)
		}
	}

	_ = cleanupLinks()
	defer func() {
		if err := cleanupLinks(); err != nil {
			fmt.Printf("cleanup warning: %v\n", err)
		}
	}()

	if err := ensureBPFFS(); err != nil {
		return err
	}
	if err := setupLinks(); err != nil {
		return err
	}

	params, err := cubeVSParams()
	if err != nil {
		return err
	}
	fmt.Printf("params: cubegw0_ifindex=%d node_ifindex=%d\n", params.Cubegw0Ifindex, params.NodeIfindex)

	fmt.Println("cubevs.Init")
	if err := cubevs.Init(params); err != nil {
		return fmt.Errorf("cubevs.Init failed: %w", err)
	}

	tap, err := net.InterfaceByName(tapDev)
	if err != nil {
		return err
	}
	fmt.Println("cubevs.AttachFilter")
	if err := cubevs.AttachFilter(uint32(tap.Index)); err != nil {
		return fmt.Errorf("cubevs.AttachFilter failed: %w", err)
	}

	fmt.Println("cubevs.SetSNATIPs")
	if err := cubevs.SetSNATIPs([]*cubevs.SNATIP{{Ifindex: int(params.NodeIfindex), IP: net.ParseIP(nodeIP)}}); err != nil {
		return fmt.Errorf("cubevs.SetSNATIPs failed: %w", err)
	}

	fmt.Println("cubevs.AddTAPDevice")
	if err := cubevs.AddTAPDevice(uint32(tap.Index), net.ParseIP(mvmIP), "cubevs-smoke", 1, cubevs.MVMOptions{}); err != nil {
		return fmt.Errorf("cubevs.AddTAPDevice failed: %w", err)
	}

	fmt.Println("cubevs.AddPortMapping")
	if err := cubevs.AddPortMapping(uint32(tap.Index), listenPort, hostPort); err != nil {
		return fmt.Errorf("cubevs.AddPortMapping failed: %w", err)
	}

	if err := verifyMaps(uint32(tap.Index)); err != nil {
		return err
	}
	if err := verifyAttach(); err != nil {
		return err
	}
	return verifyTraffic()
}

func setupLinks() error {
	if err := run("ip", "link", "set", "lo", "up"); err != nil {
		return err
	}
	for _, pair := range [][2]string{
		{cubeGW, cubeGWPeer},
		{nodeNIC, nodePeer},
		{tapDev, tapPeer},
	} {
		if err := run("ip", "link", "add", pair[0], "type", "veth", "peer", "name", pair[1]); err != nil {
			return err
		}
		if err := run("ip", "link", "set", pair[0], "up"); err != nil {
			return err
		}
		if err := run("ip", "link", "set", pair[1], "up"); err != nil {
			return err
		}
	}

	if err := run("ip", "addr", "add", cubeGWIP+"/24", "dev", cubeGW); err != nil {
		return err
	}
	if err := run("ip", "addr", "add", "192.0.2.2/24", "dev", cubeGWPeer); err != nil {
		return err
	}
	if err := run("ip", "addr", "add", mvmInnerIP+"/30", "dev", tapPeer); err != nil {
		return err
	}
	if err := run("ip", "addr", "add", nodePeerIP+"/24", "dev", nodePeer); err != nil {
		return err
	}
	return nil
}

func cubeVSParams() (cubevs.Params, error) {
	cgw, err := net.InterfaceByName(cubeGW)
	if err != nil {
		return cubevs.Params{}, err
	}
	nic, err := net.InterfaceByName(nodeNIC)
	if err != nil {
		return cubevs.Params{}, err
	}
	peer, err := net.InterfaceByName(nodePeer)
	if err != nil {
		return cubevs.Params{}, err
	}

	return cubevs.Params{
		MVMInnerIP:         net.ParseIP(mvmInnerIP),
		MVMMacAddr:         mustMAC("20:90:6f:fc:fc:fc"),
		MVMGatewayIP:       net.ParseIP(mvmGWIP),
		Cubegw0Ifindex:     uint32(cgw.Index),
		Cubegw0IP:          net.ParseIP(cubeGWIP),
		Cubegw0MacAddr:     cgw.HardwareAddr,
		NodeIfindex:        uint32(nic.Index),
		NodeIP:             net.ParseIP(nodeIP),
		NodeMacAddr:        nic.HardwareAddr,
		NodeGatewayMacAddr: peer.HardwareAddr,
	}, nil
}

func verifyMaps(tapIfindex uint32) error {
	taps, err := cubevs.ListTAPDevices()
	if err != nil {
		return fmt.Errorf("cubevs.ListTAPDevices failed: %w", err)
	}
	fmt.Printf("tap devices: %+v\n", taps)
	foundTap := false
	for _, tap := range taps {
		if tap.Ifindex == int(tapIfindex) && tap.IP.Equal(net.ParseIP(mvmIP)) && tap.ID == "cubevs-smoke" {
			foundTap = true
		}
	}
	if !foundTap {
		return fmt.Errorf("registered tap device not found in CubeVS maps: ifindex=%d ip=%s", tapIfindex, mvmIP)
	}

	ports, err := cubevs.ListPortMapping()
	if err != nil {
		return fmt.Errorf("cubevs.ListPortMapping failed: %w", err)
	}
	fmt.Printf("port mappings: %+v\n", ports)
	mapping, ok := ports[hostPort]
	if !ok || mapping.Ifindex != tapIfindex || mapping.ListenPort != listenPort {
		return fmt.Errorf("registered port mapping not found: host=%d listen=%d ifindex=%d", hostPort, listenPort, tapIfindex)
	}
	return nil
}

func verifyAttach() error {
	checks := []struct {
		dev       string
		direction string
		prog      string
	}{
		{cubeGW, "egress", "from_envoy"},
		{nodeNIC, "ingress", "from_world"},
		{"lo", "ingress", "from_world"},
		{tapDev, "ingress", "from_cube"},
	}
	for _, check := range checks {
		out, err := output("tc", "filter", "show", "dev", check.dev, check.direction)
		if err != nil {
			return err
		}
		fmt.Printf("tc filter %s %s:\n%s\n", check.dev, check.direction, out)
		if !strings.Contains(out, check.prog) {
			return fmt.Errorf("tc filter for %s %s does not contain %s", check.dev, check.direction, check.prog)
		}
	}
	return nil
}

func verifyTraffic() error {
	gwPeer, err := net.InterfaceByName(cubeGWPeer)
	if err != nil {
		return err
	}
	tap, err := net.InterfaceByName(tapDev)
	if err != nil {
		return err
	}
	node, err := net.InterfaceByName(nodeNIC)
	if err != nil {
		return err
	}

	if err := run("ip", "route", "replace", mvmIP+"/32", "dev", cubeGW); err != nil {
		return err
	}
	if err := run("ip", "neigh", "replace", mvmIP, "lladdr", gwPeer.HardwareAddr.String(), "dev", cubeGW); err != nil {
		return err
	}
	if err := run("ip", "route", "replace", mvmGWIP+"/32", "dev", tapPeer); err != nil {
		return err
	}
	if err := run("ip", "neigh", "replace", mvmGWIP, "lladdr", tap.HardwareAddr.String(), "dev", tapPeer); err != nil {
		return err
	}
	if err := run("ip", "route", "replace", externalIP+"/32", "dev", tapPeer); err != nil {
		return err
	}
	if err := run("ip", "neigh", "replace", externalIP, "lladdr", tap.HardwareAddr.String(), "dev", tapPeer); err != nil {
		return err
	}
	if err := run("ip", "route", "replace", nodeIP+"/32", "dev", nodePeer); err != nil {
		return err
	}
	if err := run("ip", "neigh", "replace", nodeIP, "lladdr", node.HardwareAddr.String(), "dev", nodePeer); err != nil {
		return err
	}

	if err := triggerAndAssert("from_envoy egress redirect", tapPeer, "rx", "ping", "-c", "2", "-W", "1", "-I", cubeGW, mvmIP); err != nil {
		return err
	}
	if err := triggerAndAssert("from_cube ingress outbound SNAT redirect", nodePeer, "rx", "ping", "-c", "2", "-W", "1", "-I", tapPeer, externalIP); err != nil {
		return err
	}
	if err := triggerAndAssert("from_world ingress port mapping", tapPeer, "rx", "bash", "-lc", fmt.Sprintf("timeout 2 bash -c 'cat < /dev/tcp/%s/%d'", nodeIP, hostPort)); err != nil {
		return err
	}
	dumpTCStats()
	return nil
}

func triggerAndAssert(label, observeDev, observeDirection string, cmd string, args ...string) error {
	before, beforeRaw, err := linkPackets(observeDev, observeDirection)
	if err != nil {
		return err
	}
	fmt.Printf("traffic trigger %s: %s %s before=%d\n", label, observeDev, observeDirection, before)
	out, err := output(cmd, args...)
	if err != nil {
		fmt.Printf("traffic trigger command failed as expected for one-way smoke traffic: %v\n%s\n", err, out)
	}
	time.Sleep(200 * time.Millisecond)
	after, afterRaw, err := linkPackets(observeDev, observeDirection)
	if err != nil {
		return err
	}
	fmt.Printf("traffic trigger %s: %s %s after=%d\n", label, observeDev, observeDirection, after)
	fmt.Printf("ip -s before %s:\n%s\n", observeDev, beforeRaw)
	fmt.Printf("ip -s after %s:\n%s\n", observeDev, afterRaw)
	if after <= before {
		return fmt.Errorf("link packet counter did not increase for %s on %s %s: before=%d after=%d", label, observeDev, observeDirection, before, after)
	}
	return nil
}

func linkPackets(dev, direction string) (uint64, string, error) {
	out, err := output("ip", "-s", "-j", "link", "show", "dev", dev)
	if err != nil {
		return 0, out, err
	}
	var links []struct {
		Stats64 struct {
			RX struct {
				Packets uint64 `json:"packets"`
			} `json:"rx"`
			TX struct {
				Packets uint64 `json:"packets"`
			} `json:"tx"`
		} `json:"stats64"`
	}
	if err := json.Unmarshal([]byte(out), &links); err != nil {
		return 0, out, err
	}
	if len(links) == 0 {
		return 0, out, fmt.Errorf("ip link show returned no link for %s", dev)
	}
	switch direction {
	case "rx":
		return links[0].Stats64.RX.Packets, out, nil
	case "tx":
		return links[0].Stats64.TX.Packets, out, nil
	default:
		return 0, out, fmt.Errorf("unknown link counter direction %q", direction)
	}
}

func dumpTCStats() {
	for _, check := range []struct {
		dev       string
		direction string
	}{
		{cubeGW, "egress"},
		{nodeNIC, "ingress"},
		{"lo", "ingress"},
		{tapDev, "ingress"},
	} {
		out, err := output("tc", "-s", "filter", "show", "dev", check.dev, check.direction)
		if err != nil {
			fmt.Printf("tc -s filter %s %s failed: %v\n%s\n", check.dev, check.direction, err, out)
			continue
		}
		fmt.Printf("tc -s filter %s %s:\n%s\n", check.dev, check.direction, out)
	}
}

func ensureBPFFS() error {
	if err := exec.Command("mountpoint", "-q", "/sys/fs/bpf").Run(); err == nil {
		return nil
	}
	return run("mount", "-t", "bpf", "bpf", "/sys/fs/bpf")
}

func cleanupLinks() error {
	var outErr error
	for _, dev := range []string{cubeGW, nodeNIC, tapDev} {
		if out, err := output("ip", "link", "del", dev); err != nil && !strings.Contains(out, "Cannot find device") {
			outErr = errors.Join(outErr, fmt.Errorf("delete %s: %w: %s", dev, err, strings.TrimSpace(out)))
		}
	}
	return outErr
}

func mustMAC(value string) net.HardwareAddr {
	mac, err := net.ParseMAC(value)
	if err != nil {
		panic(err)
	}
	return mac
}

func run(name string, args ...string) error {
	out, err := output(name, args...)
	if err != nil {
		return fmt.Errorf("%s %s failed: %w\n%s", name, strings.Join(args, " "), err, out)
	}
	return nil
}

func output(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}
