package ovs_test

import (
	"ciccni/pkg/openflow"
	"ciccni/pkg/ovs"

	"testing"
)

const (
	defaultOvsDBPath = "/run/openvswitch/db.sock"
)

func TestConnection(t *testing.T) {
	conn, _ := ovs.NewOVSDBConnectionUDS("")
	ovsBridgeClient := ovs.NewOVSBridge("test-ovs", "system", conn)
	ovsBridgeClient.Create()

	ovsBridgeClient.CreatePort("b-ns1", "b-ns1", nil)
	ovsBridgeClient.CreatePort("b-ns2", "b-ns2", nil)
	ofClient := openflow.NewClient("test-ovs")
	ofClient.InstallPodFlows()

}