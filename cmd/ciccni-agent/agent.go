package main

import (
	"ciccni/pkg/agent"
	"ciccni/pkg/cniserver"
	k8sclient "ciccni/pkg/k8s-client"
	"ciccni/pkg/openflow"
	"ciccni/pkg/ovs"
	"fmt"
	"time"

	"k8s.io/klog"
)

const (
	informerDefaultResync time.Duration = 30 * time.Second
)
func run(opts *Options) error {
	// 假定在各个机器上已经安装并且成功启动的ovs服务
	ovsdbConnection, err := ovs.NewOVSDBConnectionUDS("")
	if err != nil {
		return fmt.Errorf("error connecting OVSDB: %v", err)
	}
	defer ovsdbConnection.Close()

	// 创建ovs网桥
	ovsBridgeClient := ovs.NewOVSBridge(opts.config.OVSBridge, opts.config.OVSDatapathType, ovsdbConnection)
	err = ovsBridgeClient.Create()
	if err != nil {
		return fmt.Errorf("error create ovs bridge: %v", err)
	}
	
	stopCh := make(chan struct{})

	clientset, err2 := k8sclient.CreateClient()
	// _ = informers.NewSharedInformerFactory(clientset, informerDefaultResync)

	if err2 != nil {
		klog.Fatal("[agent.go]-[run]-创建clientset失败")
	}

	ifaceStore := agent.NewInterfaceStore()

	ofClient := openflow.NewClient(opts.config.OVSBridge)
	

	agentInitialize := agent.NewInitializer(clientset, ovsBridgeClient, ifaceStore, ofClient, opts.config.HostGateway, opts.config.DefaultMTU)
	err2 = agentInitialize.Initialize()
	if err2 != nil {
		klog.Errorf("[agent.go]-[run]-初始化agent失败, err=%s", err)
		return err2
	}

	nodeConfig := agentInitialize.GetNodeConfig()

	// default CNISocket = /var/run/ciccni/cni.sock
	// 启动rpc服务器
	cniRPCServer := cniserver.New(
		opts.config.CNISocket, 
		nodeConfig,
		opts.config.DefaultMTU, 
		opts.config.HostProcPathPrefix,
		ovsBridgeClient,
		ofClient,
		ifaceStore,
		clientset,
	) 

	go cniRPCServer.Run(stopCh)

	<- stopCh

	return nil
}
