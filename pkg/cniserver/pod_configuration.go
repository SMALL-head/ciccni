package cniserver

import (
	"ciccni/pkg/agent"
	"ciccni/pkg/agent/util"
	"ciccni/pkg/ovs"
	"ciccni/pkg/tctools"
	"encoding/json"
	"net"

	"github.com/containernetworking/cni/pkg/types"
	types040 "github.com/containernetworking/cni/pkg/types/040"
	types100 "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/ipam"
	"github.com/containernetworking/plugins/pkg/ns"

	"github.com/florianl/go-tc/core"
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

// setipVethPair 创建veth pair，一端放入容器中，另一端放入host中。tcArgs为限速配置，如果为nil则不进行限速配置
// 此处的netns应该为容器中的命名空间
func setupVethPair(podName string, podNamespace string, ifname string, netns ns.NetNS, MTU int, tcArgs *tctools.TCArgs) (hostIface *types100.Interface, containerIface *types100.Interface, err error) {
	hostVethName := util.GenerateContainerInterfaceName(podName, podNamespace)
	hostIface, containerIface = &types100.Interface{}, &types100.Interface{}

	if err := netns.Do(func(hostNS ns.NetNS) error {
		hostVeth, containerVeth, err := ip.SetupVethWithName(ifname, hostVethName, MTU, "", hostNS)
		if err != nil {
			return err
		}

		// todo: 为容器中的网络接口配置限速，配速大小应该由pod的资源需求来决定，将这个值作为参数传入
		// 限速配置错误不影响正常执行CNI的流程，仅打印日志
		// 可以配置tc的网络接口有两个，其中一个是在容器中的网络接口，另一个是在host中的网络接口。
		// 但是经过测试，发现在host段配置tc后会产生大量的丢包，因此选择在容器中进行配置。在容器中进行tc配置后如果需要进行动态修改会有些麻烦
		if tcArgs != nil {
			// rate := uint32(100000000)
			err = configureTC(containerVeth.Name, tcArgs.Rate, tcArgs.Burst)
			if err != nil {
				klog.Warningf("[setupVethPair]-[configureTC]-配置容器中的网络接口限速失败, err=%s", err)
			}
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
	if err := netns.Do(func(_ ns.NetNS) error {
		klog.Infof("[configureContainerAddr]-配置容器地址, containerInterface.Name=%s", containerInterface.Name)
		containerVeth, err := net.InterfaceByName(containerInterface.Name)
		if err != nil {
			klog.Errorf("[cniserver.go]-[configureContainerAddr]-Failed to find container interface %s in ns %s", containerInterface.Name, netns.Path())
			return err
		}
		resultJSON, _ := json.Marshal(result) // for logging
		klog.Infof("[configureContainerAddr]-分配ip地址, type100.result=%s", resultJSON)
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

func configureTC(ifName string, rate uint32, burst uint32) error {
	err := tctools.CreateRootHTB(ifName)
	if err != nil {
		klog.Errorf("[configureTC]-创建root htb失败, err = %s", err)
		return err
	}

	classHandle := core.BuildHandle(0x1, 0x1)
	qdiscRootHandle := core.BuildHandle(0x1, 0x0)

	err = tctools.CreateHTBClass(ifName, qdiscRootHandle, classHandle, rate, burst)
	if err != nil {
		klog.Errorf("[configureTC]-创建htb class失败, err = %s", err)
		return err
	}

	// todo: 高级TC配置需要增加过滤器等方式，目前仅仅是配置了class，然后查看在容器内这些配置是否生效
	err = tctools.AddTCFilterWithDstCidr(ifName, qdiscRootHandle, "10.244.0.0/16", classHandle, uint16(1))
	if err != nil {
		klog.Errorf("[configureTC]-添加过滤器失败, err = %s", err)
		return err
	}
	return nil

}
