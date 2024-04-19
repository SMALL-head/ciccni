package cniserver

import (
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
	types040 "github.com/containernetworking/cni/pkg/types/040"
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
	hostProcPathPrefix string) *CniServer {
	return &CniServer{socketAddr: cniSocket, nodeConfig: nodeConfig, defaultMTU: defaultMTU}
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

	result := &types040.Result{CNIVersion: cniConfig.CNIVersion}
	netNS := cniServer.hostNetNSPath(cniConfig.Netns)

	// 通过ipam获取可用ip
	ipamRes, err := ipam.ExecIPAMAdd(cniConfig.CniCmdArgs, cniConfig.IPAM.Type) // 注意，IPAM.Type会在stdindata中提供
	if err != nil {
		klog.Errorf("[cniserver.go]-[cmdAdd]-无法通过ipam生成ip address")
		return cniServer.ipamFailureResponse(err), nil
	}
	result.IPs = ipamRes.IPs
	result.Routes = ipamRes.Routes


	podName := string(cniConfig.K8S_POD_NAME)
	podNamespace := string(cniConfig.K8S_POD_NAMESPACE)
	configureInterface(cniServer.ovsBridgeClient, 
		cniServer.ofClient,
		cniServer.ifaceStore, 
		podName, podNamespace, 
		cniConfig.ContainerId,
		netNS,
		cniConfig.Ifname,
		cniConfig.MTU,
		result,
	)
	
	// 配置容器网络接口
	resp := &pb.CniCmdResponse{CniResult: nil}
	return resp, nil
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
	return cniServer.hostProcPathPrefix + nsPath
}

func (cniServer *CniServer) ipamFailureResponse(err error) *pb.CniCmdResponse {
	return cniServer.generateCNIErrorResponse(
		pb.ErrorCode_IPAM_FAILURE,
		fmt.Sprintf("无法生成ip address, err=%s", err),
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
	result * types040.Result,
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
	result.Interfaces = []*types040.Interface{hostIface, containerIface}

	// 3. veth接入ovs网桥中
	// 3.1 构建InterfaceConfig，这个变量一方面用于创建ovs port，另一方面会写入local cache中
	containerConfig := buildContainerConfig(containerID, podName, podNamespace, containerIface, result.IPs)
	// 3.2 创建ovs port
	ovsPortName := hostIface.Name // 注意，这个名字是通过podName+podNamespace生成的
	klog.Infof("[cniserver.go]-[configureInterface]-3.2 adding ovs port %s for container %s", ovsPortName, containerID)
	_, err = setupContainerOVSPort(ovsBridge, containerConfig, ovsPortName)
	if err != nil {
		return err
	}

	// 4. 配置ip
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
	ifaceStore.AddInterface(containerConfig.IfaceName, containerConfig)
	return nil
}

// buildContainerConfig 返回interfaceConfig，返回值用于构造网桥端口，同时写入local cache中
func buildContainerConfig(containerID, podName, podNamespace string, containerIface *types040.Interface, IPs []*types040.IPConfig) *agent.InterfaceConfig {
	containerIP, err := parseContainerIP(IPs)
	if err != nil {
		klog.Errorf("[cniserver.go]-[buildContainerConfig]-获取ip异常, err=%s", err)
		return nil
	}
	containerMAC, _ := net.ParseMAC(containerIface.Mac)
	return agent.NewContainerInterfaceConfig(containerID, podName, podNamespace, containerIface.Sandbox, containerMAC, containerIP)
}

func parseContainerIP(IPs []*types040.IPConfig) (net.IP, error) {
	for _, ipc := range IPs {
		if ipc.Version == "4" {
			return ipc.Address.IP, nil
		}
	}
	return nil, errors.New("failed to find a valid IP address")
}