package cniserver

import (
	"ciccni/pkg/agent"
	"ciccni/pkg/ovs"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"strings"

	"github.com/containernetworking/cni/pkg/types"
	types040 "github.com/containernetworking/cni/pkg/types/040"
	types100 "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/ipam"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/j-keck/arping"
	"k8s.io/klog/v2"
)

const (
	interfaceNameLength   = 15
	podNamePrefixLength   = 8
	containerKeyConnector = `-`
)


type k8sArgs struct {
	types.CommonArgs
	K8S_POD_NAME               types.UnmarshallableString
	K8S_POD_NAMESPACE          types.UnmarshallableString
	K8S_POD_INFRA_CONTAINER_ID types.UnmarshallableString
}

// setipVethPair 创建veth pair，一端放入容器中，另一端放入host中
// 此处的netns应该为容器中的命名空间
func setupVethPair(podName string, podNamespace string, ifname string, netns ns.NetNS, MTU int) (hostIface *types100.Interface, containerIface *types100.Interface, err error) {
	hostVethName := generateContainerInterfaceName(podName, podNamespace)
	hostIface, containerIface = &types100.Interface{}, &types100.Interface{}

	if err := netns.Do(func(hostNS ns.NetNS) error {
		hostVeth, containerVeth, err := ip.SetupVethWithName(ifname, hostVethName, MTU, "", hostNS)
		if err != nil {
			return err
		}
		klog.Infof("[pod_configuration.go]-[setupVethPair]-创建interface host: %s & interface container %s", hostVeth.Name, containerVeth.Name)
		containerIface.Name = containerVeth.Name
		containerIface.Mac = containerVeth.HardwareAddr.String()
		containerIface.Sandbox = netns.Path()
		hostIface.Name = hostVeth.Name
		hostIface.Mac = hostVeth.HardwareAddr.String()
		return nil

	}); err != nil {
		return nil, nil, err
	}
	return hostIface, containerIface, nil
}


// Calculates a suitable interface name using the pod namespace and pod name. The output should be
// deterministic (so that multiple calls to GenerateContainerInterfaceName with the same parameters
// return the same value). The output should have length interfaceNameLength (15). The probability of
// collision should be neglectable.
func generateContainerInterfaceName(podName string, podNamespace string) string {
	hash := sha1.New()
	podID := fmt.Sprintf("%s/%s", podNamespace, podName)
	io.WriteString(hash, podID)
	podKey := hex.EncodeToString(hash.Sum(nil))
	name := strings.Replace(podName, "-", "", -1)
	if len(name) > podNamePrefixLength {
		name = name[:podNamePrefixLength]
	}
	podKeyLength := interfaceNameLength - len(name) - len(containerKeyConnector)
	return strings.Join([]string{name, podKey[:podKeyLength]}, containerKeyConnector)
}

func setupContainerOVSPort(ovsBridge ovs.OVSBridgeClient, containerConfig *agent.InterfaceConfig, ovsPortName string) (string, error) {
	ovsAttachInfo := agent.BuildOVSPortExternalIDs(containerConfig)
	portUUID, err := ovsBridge.CreatePort(ovsPortName, ovsPortName, ovsAttachInfo)
	if err != nil {
		klog.Errorf("[cniserver.go]-[setupContainerOVSPort]-创建ovs port失败, name=%s, err:%s", ovsPortName, err)
		return "", err
	}
	return portUUID, nil
	
}

func removeContainerLink(containerID string, containerNetns string, ifname string) error {
	if err := ns.WithNetNSPath(containerNetns, func(_ ns.NetNS) error {
		var err error
		_, err = ip.DelLinkByNameAddr(ifname)
		if err != nil && err == ip.ErrLinkNotFound {
			// Not found link should return success for deletion
			klog.V(2).Infof("Interface %s not found in netns %s", ifname, containerNetns)
			return nil
		}
		return err
	}); err != nil {
		klog.Errorf("Failed to delete interface %s of container %s: %v", ifname, containerID, err)
		return err
	}
	
	return nil
}

func configureContainerAddr(netns ns.NetNS, containerInterface *types100.Interface, result *types100.Result) error {
	if err := netns.Do(func (_ ns.NetNS) error {
		containerVeth, err := net.InterfaceByName(containerInterface.Name)
		if err != nil {
			klog.Errorf("[cniserver.go]-[configureContainerAddr]-Failed to find container interface %s in ns %s", containerInterface.Name, netns.Path())
			return err
		}
		if err := ipam.ConfigureIface(containerInterface.Name, result); err != nil {
			klog.Errorf("[pod_configuration.go]-[configureContainerAddr]-ipam.ConfigureIface失败, err=%s", err)
			return err
		}
		result040Interface, _ := result.GetAsVersion("0.4.0")
		result040, _ := types040.GetResult(result040Interface)
		for _, ipc := range result040.IPs {
			if ipc.Version == "4" {
				arping.GratuitousArpOverIface(ipc.Address.IP, *containerVeth)
			}
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}