package cniserver

import (
	"bytes"
	"ciccni/pkg/agent"
	"ciccni/pkg/apis/cni/pb"
	"ciccni/pkg/cniserver/ipam"
	"ciccni/pkg/openflow"
	"ciccni/pkg/ovs"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"

	"github.com/containernetworking/cni/pkg/types"
	types100 "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/ns"
	"google.golang.org/grpc"
	"k8s.io/klog/v2"
)

type CniServer struct {
	pb.UnimplementedCniServer
	socketAddr string
	nodeConfig *agent.NodeConfig
	defaultMTU int
	hostProcPathPrefix string
	ovsBridgeClient ovs.OVSBridgeClient
	ofClient openflow.Client
	ifaceStore agent.InterfaceStore
}

func New(cniSocket string, 
	nodeConfig *agent.NodeConfig,
	defaultMTU int,
	hostProcPathPrefix string,
	ovsBridgeClient ovs.OVSBridgeClient,
	ofClient openflow.Client,
	ifaceStore agent.InterfaceStore) *CniServer {
	return &CniServer{
		socketAddr: cniSocket, 
		nodeConfig: nodeConfig, 
		defaultMTU: defaultMTU, 
		hostProcPathPrefix: hostProcPathPrefix,
		ovsBridgeClient: ovsBridgeClient,
		ofClient: ofClient,
		ifaceStore: ifaceStore,
	}
}

// Run 启动cniServer，主要是将rpc服务器绑定到unix域套接字上
func (cniServer *CniServer) Run(stopCh <-chan struct{}) {
	klog.Infoln("[cniserver.go]-[Run]-启动cniServer")
	defer klog.Infoln("[cniserver.go]-[Run]-关闭cniServer")
	server := grpc.NewServer()
	pb.RegisterCniServer(server, cniServer)

	// 将server连接到unix域套接字
	_ = os.Remove(cniServer.socketAddr) // 提前删除，防止出现bind error
	listener, err := net.Listen("unix", cniServer.socketAddr) // 在调用unix域套接字监听时，sock文件会被自动创建
	klog.Infof("[cniserver.go]-[Run]-在%s上监听rpc服务器socket", cniServer.socketAddr)
	if err != nil {
		klog.Errorf("[cniserver.go]-[Run]-连接到unix://%s错误，err=%v", cniServer.socketAddr, err)
		os.Exit(1)
	}
	go func() {
		if err := server.Serve(listener); err != nil {
			klog.Errorf("Failed to serve connections: %v", err)
		}
	}()

	<-stopCh
}

func (cniServer *CniServer) CmdAdd(ctx context.Context, request *pb.CniCmdRequest) (*pb.CniCmdResponse, error) {
	klog.Infof("[cniserver.go]-[CmdAdd]-接受到rpc请求，ContainerId=%s, Netns=%s, Ifname=%s, Args=%s, Path=%s, NetworkConfiguration=%s", 
	request.CniArgs.ContainerId, request.CniArgs.Netns, request.CniArgs.Ifname, request.CniArgs.Args, request.CniArgs.Path, request.CniArgs.NetworkConfiguration)
	cniConfig, response := cniServer.checkReuquestMessage(request)
	if response != nil {
		return response, nil
	}

	result := &types100.Result{CNIVersion: types100.ImplementedSpecVersion}
	netNS := cniServer.hostNetNSPath(cniConfig.Netns)

	// 通过ipam获取可用ip
	ipamRes, err := ipam.ExecIPAMAdd(cniConfig.CniCmdArgs, cniConfig.IPAM.Type) // 注意，IPAM.Type会在stdindata中提供
	if err != nil {
		klog.Errorf("[cniserver.go]-[cmdAdd]-无法通过ipam生成ip address")
		return cniServer.ipamFailureResponse(err), nil
	}
	result.IPs = ipamRes.IPs
	result.Routes = ipamRes.Routes

	// result.IPs中需要设置对应的interface指针
	updateResultIfaceConfig(result, cniServer.nodeConfig.Gateway.IP)

	podName := string(cniConfig.K8S_POD_NAME)
	podNamespace := string(cniConfig.K8S_POD_NAMESPACE)

	klog.Infof("[CmdAdd]-configureInterface, netNS= %s", netNS)

	// 容器内网络接口eth0配置ip地址
	err = configureInterface(cniServer.ovsBridgeClient, 
		cniServer.ofClient,
		cniServer.ifaceStore, 
		podName, podNamespace, 
		cniConfig.ContainerId,
		netNS,
		cniConfig.Ifname,
		cniConfig.MTU,
		result,
	)
	if err != nil {
		klog.Errorf("[CmdAdd]-configureInterface失败， err = %s", err)
		return cniServer.configureInterfaceFailureResponse(err), nil
	}

	result.DNS = cniConfig.DNS
	
	// 封装result
	var resultBytes bytes.Buffer
	resultAsCurrent, _ := result.GetAsVersion(cniConfig.CNIVersion) // 不妨假设配置项中的version一定是正确的
	resultAsCurrent.PrintTo(&resultBytes)
	klog.Infof("[CmdAdd]-CmdAdd request success, resp=%s", resultBytes.String())
	resp := &pb.CniCmdResponse{CniResult: resultBytes.Bytes()}
	return resp, nil
}

func (cniServer *CniServer) CmdDel(ctx context.Context, request *pb.CniCmdRequest) (*pb.CniCmdResponse, error) {
	klog.Infof("[CmdDel]-接收到CmdDel参数: %s", request)
	cniConfig, response := cniServer.checkReuquestMessage(request)
	if response != nil {
		return response, nil
	}
	if err := ipam.ExecIPAMDelete(cniConfig.CniCmdArgs, cniConfig.IPAM.Type); err != nil {
		klog.Errorf("[CmdDel]-释放IPAM中ip地址失败, err=%s", err)
		return cniServer.ipamFailureResponse(err), nil
	}

	podName := string(cniConfig.K8S_POD_NAME)
	podNamespace := string(cniConfig.K8S_POD_NAMESPACE)
	netNS := cniServer.hostNetNSPath(cniConfig.Netns)
	if err := removeInterfaces(
		cniServer.ovsBridgeClient,
		cniServer.ofClient,
		podName,
		podNamespace,
		cniServer.ifaceStore,
		cniConfig.ContainerId,
		netNS,
		cniConfig.Ifname,
	); err != nil {
		klog.Errorf("[CmdDel]-removeInterface err: %s", err)
		return cniServer.configureInterfaceFailureResponse(err), nil
	}
	return &pb.CniCmdResponse{
		CniResult: []byte(""),
	}, nil
}

// checkRequestMessage 解析request，分出CNIConfig；若解析过程出错，会直接返回一个CniCmdResponse
func (cniServer *CniServer) checkReuquestMessage(request *pb.CniCmdRequest) (*CNIConfig, *pb.CniCmdResponse) {
	cniConfig, err := cniServer.loadNetworkConfig(request)
	if err != nil {
		return nil, cniServer.generateCNIErrorResponse(
			pb.ErrorCode_DECODING_FAILURE,
			"fail to decode network config",
		)
	}
	cniVersion := cniConfig.CNIVersion
	if !cniServer.isCNIVersionSupported(cniVersion) {
		return nil, cniServer.generateCNIErrorResponse(
			pb.ErrorCode_UNSUPPORTED_FIELD,
			"unsupprted cniVersion provided: " + cniVersion,
		)
	}

	return cniConfig, nil
}

// loadNetworkConfig 解析CNiArgs参数
func (cniServer *CniServer) loadNetworkConfig(request *pb.CniCmdRequest) (*CNIConfig, error) {
	cniConfig := &CNIConfig{}
	cniConfig.CniCmdArgs = request.CniArgs // 原装内容填充

	// NetworkConfiguration是一个[]byte，它来自StdinData
	if err := json.Unmarshal(request.CniArgs.NetworkConfiguration, cniConfig); err != nil {
		return cniConfig, err
	} 
	cniConfig.k8sArgs = &k8sArgs{}
	if err := types.LoadArgs(request.CniArgs.Args, cniConfig.k8sArgs); err != nil {
		return cniConfig, err
	}
	cniConfig.NetworkConfig.IPAM.Subnet = cniServer.nodeConfig.PodCIDR.String()
	cniConfig.NetworkConfiguration, _ = json.Marshal(cniConfig.NetworkConfig)
	if cniConfig.MTU == 0 {
		cniConfig.MTU = cniServer.defaultMTU
	}
	return cniConfig, nil
}

func (cniServer *CniServer) generateCNIErrorResponse(cniErrorCode pb.ErrorCode, cniErrorMsg string) *pb.CniCmdResponse {
	return &pb.CniCmdResponse{
		Error: &pb.Error{
			Code: cniErrorCode,
			Message: cniErrorMsg,
		},
	}
}

// TODO: 目前仅支持0.3.0，日后可考虑兼容性问题？
func (cniServer *CniServer) isCNIVersionSupported(cniVersion string) bool {
	return true
}

func (cniServer *CniServer) hostNetNSPath(nsPath string) string {
	if nsPath == "" { return "" }
	klog.Infof("[hostNetNSPath]-cniServer.hostProcPathPrefix = %s", cniServer.hostProcPathPrefix)
	return cniServer.hostProcPathPrefix + nsPath
}

func (cniServer *CniServer) ipamFailureResponse(err error) *pb.CniCmdResponse {
	return cniServer.generateCNIErrorResponse(
		pb.ErrorCode_IPAM_FAILURE,
		fmt.Sprintf("无法生成ip address, err=%s", err),
	)
}

func (cniServer *CniServer) configureInterfaceFailureResponse(err error) *pb.CniCmdResponse {
	return cniServer.generateCNIErrorResponse(
		pb.ErrorCode_CONFIG_INTERFACE_FAILURE,
		fmt.Sprintf("配置网络ip异常, err=%s", err),
	)
}

func configureInterface(
	ovsBridge ovs.OVSBridgeClient, // 配置网桥端口
	ofClient openflow.Client, // 为新加入的端口配置流表规则
	ifaceStore agent.InterfaceStore, // 缓存新加入的接口
	podName string,
	podNamespace string,
	containerID string, 
	containerNetNS string,
	ifname string,
	MTU int,
	result *types100.Result,
) error {

	// success 将用于判断是否执行最后的defer function
	success := false
	defer func() {
		if !success {
			removeContainerLink(containerID, containerNetNS, ifname)
		}
	}()

	// 1. 获取容器的netns
	netns, err := ns.GetNS(containerNetNS)
	if err != nil {
		klog.Errorf("[cniserver.go]-[configureInterface]-无法获取ns %s, err=%v", containerNetNS, err)
		return err
	}
	defer netns.Close()

	// 2. 创建veth pair
	hostIface, containerIface, err := setupVethPair(podName, podNamespace, ifname, netns, MTU)
	if err != nil {
		return err
	}
	result.Interfaces = []*types100.Interface{hostIface, containerIface}

	// 3. veth接入ovs网桥中
	// 3.1 构建InterfaceConfig，这个变量一方面用于创建ovs port，另一方面会写入local cache中
	containerConfig := buildContainerConfig(containerID, podName, podNamespace, containerIface, result.IPs)
	// 3.2 创建ovs port
	ovsPortName := hostIface.Name // 注意，这个名字是通过podName+podNamespace生成的
	klog.Infof("[cniserver.go]-[configureInterface]-3.2 adding ovs port %s for container %s", ovsPortName, containerID)
	portUUID, err := setupContainerOVSPort(ovsBridge, containerConfig, ovsPortName)
	if err != nil {
		return err
	}

	// Rollback to remove OVS port if hit error in later manipulations
	defer func() {
		if !success {
			err := ovsBridge.DeletePort(portUUID)
			if err != nil {
				klog.Errorf("[configureInterface]-删除ovs上的网桥 uuid=%s失败, err = %s", portUUID, err)
			}
		}
	}()

	// 4. 配置ip
	klog.Infof("[cniserver.go]-[configureInterface]-4. 配置ip",)
	err = configureContainerAddr(netns, containerIface, result)
	if err != nil {
		klog.Errorf("[cniserver.go]-[configureInterface]-4. 配置ip出错, err=%s", err)
		return err
	}

	// 5. 配置该端口的流表规则。感觉不用配置流表规则了
	// ofPort, err := ovsBridge.GetOFPort(ovsPortName)
	// if err != nil {
	// 	klog.Errorf("[cniserver.go]-[configureInterface]-5. GetOfPort %s error, err=%s",ovsPortName, err)
	// }

	// 6. 配置信息写入local cache中
	ofPort, err := ovsBridge.GetOFPort(ovsPortName)
	if err != nil {
		klog.Errorf("Failed to get of_port of OVS interface %s: %v", ovsPortName, err)
		return err
	}
	containerConfig.OVSPortConfig = &agent.OVSPortConfig{PortUUID: portUUID, IfaceName: ovsPortName, OFPort: ofPort}
	klog.Infof("[configureInterface]-缓存接口信息, key = %s, value = %s", containerConfig.IfaceName, containerConfig.String())
	ifaceStore.AddInterface(containerConfig.IfaceName, containerConfig)
	success = true
	return nil
}

func removeInterfaces(
	ovsBridgeClient ovs.OVSBridgeClient, 
	ofCient openflow.Client,
	podName string,
	podNamepsace string,
	ifaceStore agent.InterfaceStore,
	containerID string,
	containerNetns string,
	ifname string,
) error {
	if containerNetns != "" {
		if err := removeContainerLink(containerID, containerNetns, ifname); err != nil {
			return err
		}
		klog.Infof("Target netns not specified, not removing veth pair")
	}
	interfaceConfig, found  := ifaceStore.GetContainerInterface(podName, podNamepsace)
	if !found {
		klog.Errorf("[removeInterfaces]-无法在local cache中找到port, containerID = %s", containerID)
		return nil
	}

	portUUID := interfaceConfig.PortUUID
	ovsPortName := interfaceConfig.IfaceName
	klog.Infof("[removeInterfaces]-删除ovs port, UUID = %s, containerID = %s, ovsPortName = %s", portUUID, containerID, ovsPortName)
	if err := ovsBridgeClient.DeletePort(portUUID); err != nil {
		klog.Errorf("[removeInterfaces]-删除ovs port异常, portUUID = %s, portName = %s, err = %s", portUUID, ovsPortName, err)
		return err
	}
	ifaceStore.DeleteInterface(ovsPortName)
	klog.Infof("[removeInterfaces]-删除ovs port成功, containerID = %s", containerID)
	return nil
}


// buildContainerConfig 返回interfaceConfig，返回值用于构造网桥端口，同时写入local cache中
func buildContainerConfig(containerID, podName, podNamespace string, containerIface *types100.Interface, IPs []*types100.IPConfig) *agent.InterfaceConfig {
	containerIP, err := parseContainerIP(IPs)
	if err != nil {
		klog.Errorf("[cniserver.go]-[buildContainerConfig]-获取ip异常, err=%s", err)
		return nil
	}
	containerMAC, _ := net.ParseMAC(containerIface.Mac)
	return agent.NewContainerInterfaceConfig(containerID, podName, podNamespace, containerIface.Sandbox, containerMAC, containerIP)
}

func parseContainerIP(IPs []*types100.IPConfig) (net.IP, error) {
	for _, ipc := range IPs {
		if ipc.Address.IP.To4() != nil {
			return ipc.Address.IP, nil
		}
	}
	return nil, errors.New("failed to find a valid IP address")
}

func updateResultIfaceConfig(result *types100.Result, defaultV4Gateway net.IP) {
	for _, ipc := range result.IPs {
		// type IPConfig struct {
		// 		Index into Result structs Interfaces list
		//		Interface *int               ------> 这里需要配置一下为0还是1
		//		Address   net.IPNet
		//		Gateway   net.IP
		//	}

		
		// result.Interfaces[0] is host interface, and result.Interfaces[1] is container interface
		// 因此这里指定为1，表示该条IPConfig是container interface的
		ipc.Interface = types100.Int(1)

		klog.Infof("[uodateResultIfaceCOnfig]-ipc信息：%s", ipc.String())
		if ipc.Gateway == nil {
			ipn := ipc.Address
			netID := ipn.IP.Mask(ipn.Mask)
			ipc.Gateway = ip.NextIP(netID)
		}

		// 接下来我们就在result中看是否有默认路由了，
		// 如果没有，则应该在result中加入默认路由，用于寻找网关（按照我我们初版本执行流程，这里肯定是没有的）
		foundDefaultRoute := false
		defaultRouteDst := "0.0.0.0/0"
		if result.Routes != nil {
			for _, route := range result.Routes {
				if route.Dst.String() == defaultRouteDst {
					foundDefaultRoute = true
					break
				}
			}
		} else {
			result.Routes = []*types.Route{}
		}

		if !foundDefaultRoute {
			_, defaultRouteDst, _ := net.ParseCIDR(defaultRouteDst)
			result.Routes = append(result.Routes, &types.Route{Dst: *defaultRouteDst, GW: defaultV4Gateway})
		}

	}
}
