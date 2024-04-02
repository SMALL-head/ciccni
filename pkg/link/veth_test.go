package link_test

import (
	"ciccni/pkg/link"
	"ciccni/pkg/ovs"
	"net"
	"strings"
	"testing"

	types100 "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/stretchr/testify/require"
)

func CreateVeth(t *testing.T, nsPath string, ipAddrWithMask string, podNameAndNamespace string) (hostVeth *types100.Interface, containerVeth *types100.Interface){
	ns, _:= ns.GetNS(nsPath)
	splitRes := strings.Split(podNameAndNamespace, "|")
	hostVeth, containerVeth, err := link.SetupInterface(splitRes[0], splitRes[1], "veth", ns, 1500)
	require.NoError(t, err)
	ip, ipnet, err := net.ParseCIDR(ipAddrWithMask)
	ipnet.IP = ip
	require.NoError(t, err)
	err = link.ConfigureContainerAddr(ns, containerVeth, ipnet)
	require.NoError(t, err)
	return
}

func TestVethCreate(t *testing.T) {
	// 创建网桥
	client, err := ovs.NewOVSDBConnectionUDS("") // default address
	require.NoError(t, err)
	ovsBridgeClient := ovs.NewOVSBridge("br0", "", client)
	
	err = ovsBridgeClient.Create()
	require.NoError(t, err)

	hostVeth1, _ := CreateVeth(t, "/var/run/netns/ns1", "192.168.31.3/24", "pod1|n1")

	// host端加入网桥
	uuid, err := ovsBridgeClient.GetPortUUIDByIfName(hostVeth1.Name)
	ovsBridgeClient.DeletePort(uuid)
	ovsBridgeClient.CreatePort(hostVeth1.Name, hostVeth1.Name, nil) // 前两参数一般设置为同名即可
	

	hostVeth2, _ := CreateVeth(t, "/var/run/netns/ns2", "192.168.31.4/24", "pod2|n1")

	// host端加入网桥
	require.NoError(t, err)
	uuid, err = ovsBridgeClient.GetPortUUIDByIfName(hostVeth2.Name)
	ovsBridgeClient.DeletePort(uuid)
	ovsBridgeClient.CreatePort(hostVeth2.Name, hostVeth2.Name, nil) // 前两参数一般设置为同名即可
}