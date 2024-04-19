package ovs_test

import (
	"ciccni/pkg/openflow"
	"ciccni/pkg/ovs"
	"net"

	"testing"

	"github.com/stretchr/testify/require"
)

func TestConnection(t *testing.T) {
	conn, _ := ovs.NewOVSDBConnectionUDS("")
	ovsBridgeClient := ovs.NewOVSBridge("test-ovs", "system", conn)
	ovsBridgeClient.Create()

	ovsBridgeClient.CreatePort("b-ns1", "b-ns1", nil)
	ovsBridgeClient.CreatePort("b-ns2", "b-ns2", nil)
	ofClient := openflow.NewClient("test-ovs")
	portNum, _ := ovsBridgeClient.GetOFPort("b-ns1")
	dstTunIP := net.ParseIP("172.16.0.119")
	ofClient.InstallTunFlow("172.16.0.0/24", uint32(portNum), dstTunIP)

}

// TestOpenflow 测试能够通过自己写的方法成功向ovs添加一条预期的openflow表项
// 这个测试用例添加了如下流表项"table=0,priority=200,ip,in_port=b-v action=set_field:172.16.0.120->tun_dst,normal"
func TestOpenflow(t *testing.T) {
	conn, _ := ovs.NewOVSDBConnectionUDS("")
	ovsBridgeClient := ovs.NewOVSBridge("br0", "", conn)
	portNum, _ := ovsBridgeClient.GetOFPort("b-v")

	ofClient := openflow.NewClient("br0")
	dstTunIP := net.ParseIP("172.16.0.119")
	err := ofClient.InstallTunFlow("172.16.0.1", uint32(portNum), dstTunIP)
	require.NoError(t, err)
	ofClient.UninstallTunFlow()
}

// func TestDeleteOpenflow(t *testing.T) {
// 	conn, _ := ovs.NewOVSDBConnectionUDS("")
// 	ovsBridgeClient := ovs.NewOVSBridge("br0", "", conn)
// 	// portNum, _ := ovsBridgeClient.GetOFPort("b-v")

// 	ofClient := openflow.NewClient("br0")
// 	ofClient.UninstallTunFlow(openflow.IPConnectionVxlan)
	
// }