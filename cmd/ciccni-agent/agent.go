package main

import (
	"ciccni/pkg/ovs"
	"fmt"
)

type AgentConfig struct {
	CNISocket string `yaml:"cniSocket,omitempty"`
	// clientConnection specifies the kubeconfig file and client connection settings for the agent
	// to communicate with the apiserver.
	// ClientConnection componentbaseconfig.ClientConnectionConfiguration `yaml:"clientConnection"`
	// AntreaClientConnection specifies the kubeconfig file and client connection settings for the
	// agent to communicate with the Antrea Controller apiserver.
	// AntreaClientConnection componentbaseconfig.ClientConnectionConfiguration `yaml:"antreaClientConnection"`
	// Name of the OpenVSwitch bridge antrea-agent will create and use.
	// Make sure it doesn't conflict with your existing OpenVSwitch bridges.
	// Defaults to br-int.
	OVSBridge string `yaml:"ovsBridge,omitempty"`
	// Datapath type to use for the OpenVSwitch bridge created by Antrea. Supported values are:
	// - system
	// - netdev
	// 'system' is the default value and corresponds to the kernel datapath. Use 'netdev' to run
	// OVS in userspace mode. Userspace mode requires the tun device driver to be available.
	OVSDatapathType string `yaml:"ovsDatapathType,omitempty"`
	// Name of the interface antrea-agent will create and use for host <--> pod communication.
	// Make sure it doesn't conflict with your existing interfaces.
	// Defaults to gw0.
	HostGateway string `yaml:"hostGateway,omitempty"`
	// Encapsulation mode for communication between Pods across Nodes, supported values:
	// - vxlan (default)
	// - geneve
	TunnelType string `yaml:"tunnelType,omitempty"`
	// Default MTU to use for the host gateway interface and the network interface of each
	// Pod. If omitted, antrea-agent will default this value to 1450 to accomodate for tunnel
	// encapsulate overhead.
	DefaultMTU int `yaml:"defaultMTU,omitempty"`
	// Mount location of the /proc directory. The default is "/host", which is appropriate when
	// antrea-agent is run as part of the Antrea DaemonSet (and the host's /proc directory is mounted
	// as /host/proc in the antrea-agent container). When running antrea-agent as a process,
	// hostProcPathPrefix should be set to "/" in the YAML config.
	HostProcPathPrefix string `yaml:"hostProcPathPrefix,omitempty"`
	// CIDR Range for services in cluster. It's required to support egress network policy, should
	// be set to the same value as the one specified by --service-cluster-ip-range for kube-apiserver.
	// Default is 10.96.0.0/12
	ServiceCIDR string `yaml:"serviceCIDR,omitempty"`
	// Whether or not to enable IPSec (ESP) tunnel for Pod traffic across Nodes. Antrea uses Preshared
	// Key (PSK) for IKE authentication. When IPSec tunnel is enabled, the PSK value must be passed to
	// Antrea Agent through an environment variable: ANTREA_IPSEC_PSK.
	// Defaults to false.
	EnableIPSecTunnel bool `yaml:"enableIPSecTunnel,omitempty"`
}

func run(opts *Options) error {
	// 假定在各个机器上已经配置好了ovs
	ovsdbConnection, err := ovs.NewOVSDBConnectionUDS("")
	if err != nil {
		return fmt.Errorf("error connecting OVSDB: %v", err)
	}
	defer ovsdbConnection.Close()

	// 创建网桥
	ovsBridgeClient := ovs.NewOVSBridge(opts.config.OVSBridge, opts.config.OVSDatapathType, ovsdbConnection)
	err = ovsBridgeClient.Create()
	if err != nil {
		return fmt.Errorf("error create ovs bridge: %v", err)
	}
	// 启动rpc服务器
	return nil
}
