package main

import (
	"ciccni/pkg/openflow"
	"ciccni/pkg/ovs"
	"net"

	"github.com/sirupsen/logrus"
)

func main() {
	stopCh := make(chan struct{})
	conn, err := ovs.NewOVSDBConnectionUDS("")
	if err != nil {
		logrus.Errorf("NewOVSDBConnectionUDS err, err = %s", err)
		return
	}

	ovsBridgeClient := ovs.NewOVSBridge("br0", "", conn)
	portNum, err := ovsBridgeClient.GetOFPort("b-v")

	if err != nil {
		logrus.Errorf("NewOVSDBConnectionUDS err, err = %s", err)
		return
	}

	ofClient := openflow.NewClient("br0")
	dstTunIP := net.ParseIP("172.16.0.119")
	err2 := ofClient.InstallTunFlow("172.16.0.1", uint32(portNum), dstTunIP)
	if err2 != nil {
		logrus.Errorf("IntallTunFlow err, err = %s", err2)
	}
	<-stopCh
}