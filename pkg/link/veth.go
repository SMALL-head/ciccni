package link

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"strings"

	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"
	"k8s.io/klog/v2"
)

const (
	interfaceNameLength   = 15
	podNamePrefixLength   = 8
	containerKeyConnector = `-`
)

func generateRandomMAC() net.HardwareAddr {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		klog.ErrorS(err, "Failed to generate a random MAC")
	}
	// Unset the multicast bit.
	buf[0] &= 0xfe
	buf[0] |= 0x02
	return buf
}
// GenerateContainerInterfaceName 生成容器内部的借口名
func GenerateContainerInterfaceName(podName string, podNamespace string) string {
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


// SetupInterface netns传入容器的ns即可
// podName和podNamespace只用于veth的命名，netns需要传入ns对应的path
// 这个函数调用后，容器内部的veth并没有设置为up状态，需要调用ConfigureContainerAddr方法进行配置
func SetupInterface(podName string, podNamespace string, ifname string, netns ns.NetNS, MTU int) (hostIface *current.Interface, containerIface *current.Interface, err error) {
	hostVethName := GenerateContainerInterfaceName(podName, podNamespace)
	hostIface = &current.Interface{}
	containerIface = &current.Interface{}
	if err := netns.Do(func(hostNS ns.NetNS) error {
		// podMac := GenerateRandomMAC()
		ip.DelLinkByName(ifname) // 如果存在的话先删除
		hostVeth, containerVeth, err := ip.SetupVethWithName(ifname, hostVethName, MTU, "", hostNS)
		if err != nil {
			return err
		}
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

// ConfigureContainerAddr 配置容器内部的veth信息
// 找到veth设备，将其状态设置为up，同时为其配置一个ip地址。一般情况下这个ip地址需要通过ipam插件来获得
func ConfigureContainerAddr(netns ns.NetNS, containerInterface *current.Interface, ip *net.IPNet) error {
	if err := netns.Do(func (container ns.NetNS) error  {
		// containerVeth, err := net.InterfaceByName(containerInterface.Name)
		// if err != nil {
		// 	klog.Errorf("[veth.go]-[ConfigureContainerAddr]-无法获取containerInterface.Name为%s的veth, err=%v", containerInterface.Name, err)
		// 	return err
		// }
		ifName := containerInterface.Name
		link, err := netlink.LinkByName(ifName)
		if err != nil {
			klog.Errorf("[veth.go]-[ConfigureContainerAddr]-无法获取containerInterface.Name为%s的veth, err=%v", ifName, err)
			return err
		}
		if err := netlink.LinkSetUp(link); err != nil {
			klog.Errorf("[veth.go]-[ConfigureContainerAddr]-无法设置 containerInterface.Name为%s的veth 状态为 up, err=%v", ifName, err)
			return err
		}
		addr := &netlink.Addr{IPNet: ip, Label: ""}
		if err := netlink.AddrAdd(link, addr); err != nil {
			klog.Errorf("[veth.go]-[ConfigureContainerAddr]-配置containerInterface.Name为%s的veth中的ip地址错误, err=%v", ifName, err)
			return err
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

// DeleteInterface 传入容器的ns路径，然后删除里面的interface
func DeleteInterface(containerNetns string, ifname string) error {
	return ns.WithNetNSPath(containerNetns, func(nn ns.NetNS) error {
		var err error
		err = ip.DelLinkByName(ifname)
		if err != nil {
			klog.Errorf("[veth.go]-[DeleteInterface]-无法删除设备%s, err=%v", ifname, err)
			return err
		}
		return nil
	})
}