package agent

import (
	"ciccni/pkg/agent/types"
	"ciccni/pkg/iptables"
	"ciccni/pkg/link"
	"ciccni/pkg/openflow"
	"ciccni/pkg/ovs"
	"context"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/vishvananda/netlink"
	"gopkg.in/yaml.v2"
	v1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

const (
	
	NodeNameEnvKey = "NODE_NAME"
	OutInterfaceEnvKey = "OUT_INTERFACE"
	TunPortName = "tun0"
	tunOFPort = 1
	hostGatewayOFPort = 2
	maxRetryForHostLink = 5
)

type NodeConfig struct {
	NodeName string
	PodCIDR *net.IPNet
	ClusterPodCIDR *net.IPNet
	Bridge string
	*Gateway
}

type Gateway struct {
	IP net.IP
	MAC net.HardwareAddr
	Name string
}

type Initializer struct {
	k8sClient kubernetes.Interface
	nodeConfig *NodeConfig
	ovsBridgeClient ovs.OVSBridgeClient
	ifaceStore InterfaceStore
	ofClient openflow.Client
	hostGateway string
	MTU int
} 

func NewInitializer(k8sClient kubernetes.Interface, 
					ovsBridgeClient ovs.OVSBridgeClient, 
					ifaceStore InterfaceStore, 
					ofCLient openflow.Client, 
					hostGateway string, 
					MTU int) *Initializer{
	return &Initializer{
		k8sClient: k8sClient,
		ovsBridgeClient: ovsBridgeClient,
		ifaceStore: ifaceStore,
		ofClient: ofCLient,
		hostGateway: hostGateway,
		MTU: MTU,
	}
}

/** Initialize 对agent进行初始化
  - 节点信息，包括node名、podcidr等信息
  - 安装对应的ovs网桥
  - 写入基本flow，并缓存arp内容（arp内容会在后续加入节点node的时候进行修改） 
  TODO：在最初版本中，没有添加新加入节点的时候修改arp流表项；后续我们希望增加这个功能
*/
func (i *Initializer) Initialize() error {
	// 1. 初始化节点信息
	klog.Infof("[agentFunc.go]-[Initialize]-初始化节点信息")
	if err := i.initNodeLocalConfig(); err != nil {
		return err
	}

	// 2. iptables规则安装
	iptablesClient, err := iptables.NewClient(i.hostGateway, i.nodeConfig.PodCIDR.String())
	if err != nil {
		return fmt.Errorf("[Initialize] - error creating iptables client: %v", err)
	}
	outInterfaceName, err := link.GetDefaultInterface()
	if err != nil {
		klog.Errorf("[Initialize] - error getting default interface: %v", err)
	}
	if err := iptablesClient.SetUpRules(outInterfaceName); err != nil {
		return fmt.Errorf("[Initialize] - error setting up iptables rules: %v", err)
	}


	// 3. 初始化网桥
	klog.Infof("[agentFunc.go]-[Initialize]-初始化网桥")
	if err := i.setUpOVSBridge(); err != nil {
		return err
	}

	// 4. 初始flow安装
	if err := i.setUpFlow(); err != nil {
		return err
	}

	return nil
}

// initNodeLocalConfig 获取节点的名字以及对应的PodCIDR，将其写入i中
func (i *Initializer) initNodeLocalConfig() error {
	nodeName, err := getNodeName()
	if err != nil {
		return err
	}
	node, err := i.k8sClient.CoreV1().Nodes().Get(context.TODO(), nodeName, metaV1.GetOptions{})
	if err != nil {
		klog.Errorf("[initNodeLocalConfig]-未找到名为{%s}的node, err=%v", nodeName, err)
		return err
	}
	if node.Spec.PodCIDR == "" {
		klog.Errorf("Spec.PodCIDR is empty for Node %s. Please make sure --allocate-node-cidrs is enabled "+
			"for kube-controller-manager and --cluster-cidr specifies a sufficient CIDR range", nodeName)
		return fmt.Errorf("CIDR string is empty for node %s", nodeName)
	}
	_, localSubnet, err := net.ParseCIDR(node.Spec.PodCIDR)
	if err != nil {
		klog.Errorf("[initNodeLocalConfig]-解析PodCIDR错误, node.Spec.PodCIDR=%s, err=%v", node.Spec.PodCIDR, err)
		return err
	}
	// 获取大的集群pod_cidr
	kubeadmConfig, err := i.k8sClient.CoreV1().ConfigMaps("kube-system").Get(context.TODO(), "kubeadm-config", metaV1.GetOptions{})
	if err != nil {
		klog.Errorf("[initNodeLocalConfig]-获取kubeadm-config错误, err=%v", err)
		return err
	}
	clusterConfigData := kubeadmConfig.Data["ClusterConfiguration"]
	var clusterConfig types.ClusterConfig
	err = yaml.Unmarshal([]byte(clusterConfigData), &clusterConfig)
	if err != nil {
		klog.Errorf("[initNodeLocalConfig]-解析ClusterConfiguration错误, err=%v", err)
		return err
	}
	_, clusterSubnet, err := net.ParseCIDR(clusterConfig.Networking.PodSubnet)
	klog.Infof("[initNodeLocalConfig]-通过configmap获取到的clusterSubnet信息: %v", clusterSubnet)
	
	klog.Infof("[initNodeLocalConfig]-%s node对应的localSubnet为%v", nodeName, localSubnet)
	// gatewayIP := ip.NextIP(localSubnet.IP.Mask(localSubnet.Mask))
	i.nodeConfig = &NodeConfig{NodeName: nodeName, PodCIDR: localSubnet, ClusterPodCIDR: clusterSubnet}
	return nil
}

func (i *Initializer) GetNodeConfig() *NodeConfig {
	return i.nodeConfig
}

func (i *Initializer) setUpOVSBridge() error {
	// 1. 创建网桥
	// if err := i.ovsBridgeClient.Create() ; err != nil {
	// 	klog.Errorf("[agentFunc.go]-[setUpOVSBridge]-创建网桥失败, err=%v", err)
	// 	return err
	// }

	// 2. 构造ovs端口cache
	if err := i.ifaceStore.Initialize(i.ovsBridgeClient, TunPortName); err != nil {
		klog.Errorf("[agentFunc.go]-[setUpOVSBridge]-无法初始化ifaceStore, err=%v", err)
		return err
	}

	// 3. 创建ovs对应的tunnel端口
	if err := i.setUpTunnelInterface(TunPortName); err != nil {
		return err
	}

	// 4. 创建ovs中的网关接口
	err := i.setupGatewayInterface()
	if err != nil { return err }

	return nil

}


func (i *Initializer) setUpTunnelInterface(tunPortName string) error {
	tunnelIface, portExists := i.ifaceStore.GetInterface(tunPortName)
	if portExists {
		klog.V(2).Infof("[agentFunc.go]-[setUpTunnelInterface]-port %s 已经存在", tunPortName)
		return nil
	}
	tunnelPortUUID, err := i.ovsBridgeClient.CreateVXLANPort(tunPortName, tunOFPort, "")
	if err != nil {
		klog.Errorf("[agentFunc.go]-[setUpTunnelInterface]-无法创建VXLAN tunnel port %s, err=%v", tunPortName, err)
		return err
	}
	tunnelIface = NewTunnelInterface(tunPortName)
	tunnelIface.OVSPortConfig = &OVSPortConfig{IfaceName: tunPortName, PortUUID: tunnelPortUUID, OFPort: tunOFPort}
	i.ifaceStore.AddInterface(tunPortName, tunnelIface)
	return nil
}

func (i *Initializer) initOpenFlow() error {
	// 1. 写入发向每个node的arp包
	nodeList, err := i.k8sClient.CoreV1().Nodes().List(context.TODO(), metaV1.ListOptions{})
	if err != nil {
		klog.Errorf("[agentFunc.go]-[initOpenFlow]-获取所有Node列表出错")
		return err
	}

	i.constructArpOpenflow(nodeList)
	
	// 2. 在此处加入发向每个node的ip流表项
	i.constructIPTunFlow(nodeList)
	
	return nil
	
}

func (i *Initializer) constructArpOpenflow(nodeList *v1.NodeList) {
	tunDsts := make([]string, 0)

	// 遍历每一个Node，找到其IP，这个IP将会用作有关ARP的的流表项构建
	for _, node := range nodeList.Items {
		if node.Name == i.nodeConfig.NodeName {
			continue
		}

		// 有没有可能，某个Node没有NodeInternalIP呢？
		for _, address := range node.Status.Addresses {
			if address.Type == v1.NodeInternalIP {
				tunDsts = append(tunDsts, address.Address)
				break
			}
		}
	}
	if len(tunDsts) != 0 {
		i.ofClient.InstallARPFlow(tunDsts)
	}	
}

func (i *Initializer) constructIPTunFlow(nodeList *v1.NodeList) {
	for _, node := range nodeList.Items {
		if node.Name == i.nodeConfig.NodeName { // 本地node只需要为ip进行nromal操作即可
			klog.Infof("[constructIPTunFlow]-本地node ip流表安装")
			err := i.ofClient.InstallLocalIPFlow(node.Name, i.nodeConfig.PodCIDR.String())
			if err != nil {
				klog.Errorf("[constructIPTunFlow]-本地node ip安装失败, podcidr = %s, err = %s", i.nodeConfig.PodCIDR.String(), err)
			}
		} else {
			var nodeAddress net.IP
			for _, address := range node.Status.Addresses {
				if address.Type == v1.NodeInternalIP {
					nodeAddress = net.ParseIP(address.Address)
					break
				}
			}
			if nodeAddress != nil {
				klog.Infof("[constructIPTunFlow]-node: %s ip流表安装", node.Name)
				err := i.ofClient.InstallTunFlow(node.Spec.PodCIDR, 0, nodeAddress)
				if err != nil {
					klog.Errorf("[constructIPTunFlow]-node %s ip安装失败, err = %s", nodeAddress, err)
				}
			} else {
				klog.Errorf("[agentFunc.go]-[constructIPTunFlow]-为node:%s安装ip流表规则出错", node.Name)
			}
		}
	}
}

func (i *Initializer) setUpFlow() error {
	// 写入基本的openflow流表项
	if err := i.ofClient.Initialize(); err != nil {
		klog.Errorf("[Initialize]-ofClient.Initalize()失败， err = %s", err)
		return err
	}
	if err := i.initOpenFlow(); err != nil {
		return err
	}
	return nil
}

// getNodeName 尝试通过环境变量获取nodeName。注意，这个环境变量应该通过yaml文件中进行配置
func getNodeName() (string, error) {
	nodeName := os.Getenv(NodeNameEnvKey)
	if nodeName != "" {
		klog.Infof("[getNodeName]-获取到nodeName: %s", nodeName)
		return nodeName, nil
	}
	klog.Infof("[agentFunc.go]-[getNodeName]-无法通过%s获取nodeName，尝试使用hostname代替", NodeNameEnvKey)
	hostname, err := os.Hostname()
	if err != nil {
		return "", err
	}
	return hostname, nil
	
}

// setupGatewayInterface 在ovs上创建网关接口，同时向nodeConfig中写入网关接口相关的信息
func (i *Initializer) setupGatewayInterface() error {
	// 从cache中查看，若不存host Gateway port，则创建host Gateway port
	gatewayIface, portExist := i.ifaceStore.GetInterface(i.hostGateway)
	if !portExist {
		klog.Infof("[setupGateway]-在ovs上创建gateway port %s", i.hostGateway)
		gwPortUUID, err := i.ovsBridgeClient.CreateInternalPort(i.hostGateway, hostGatewayOFPort, nil)
		if err != nil {
			klog.Errorf("[setupGateway]-创建gateway port失败, gw name=%s, err=%s", i.hostGateway, err)
			return err
		}
		gatewayIface = NewGatewayInterface(i.hostGateway)
		gatewayIface.OVSPortConfig = &OVSPortConfig{IfaceName: i.hostGateway, PortUUID: gwPortUUID, OFPort: hostGatewayOFPort}
		i.ifaceStore.AddInterface(i.hostGateway, gatewayIface)
	} else {
		klog.Infof("[setupGateway]-Gateway port %s 已经存在", i.hostGateway)
	}
	klog.Infof("[setupGateway]-setting gateway interface %s MTU to %d", i.hostGateway, i.MTU)
	i.ovsBridgeClient.SetInterfaceMTU(i.hostGateway, i.MTU)

	// 阻塞等待gateway port被创建：retry max 5 times with 1s delay each time to ensure the interface is ready
	link, err := func() (netlink.Link, error) {
		for retry := 0; retry < maxRetryForHostLink; retry++ {
			if link, err := netlink.LinkByName(i.hostGateway); err != nil {
				klog.Infof("[setupGateway] - Not found host link for gateway %s, retry after 1s", i.hostGateway)
				if _, ok := err.(netlink.LinkNotFoundError); ok {
					time.Sleep(1 * time.Second)
				} else {
					return link, err
				}
			} else {
				return link, nil
			}
		}
		return nil, fmt.Errorf("[setupGateway] - link %s not found", i.hostGateway)
	}()
	if err != nil {
		klog.Errorf("[setupGateway]-获取host link失败, err=%s", err)
		return err
	}

	// gateway 激活
	if err := netlink.LinkSetUp(link); err != nil {
		klog.Errorf("[setupGateway]-Failed to set host link for %s up: %v", i.hostGateway, err)
		return err
	}

	// 配置ip地址，网关相关信息写入nodeConfig
	localSubnet := i.nodeConfig.PodCIDR
	subnetID := localSubnet.IP.Mask(localSubnet.Mask)
	gwIP := &net.IPNet{IP: ip.NextIP(subnetID), Mask: i.nodeConfig.ClusterPodCIDR.Mask}
	gwAddr := &netlink.Addr{IPNet: gwIP, Label: ""}
	gwMAC := link.Attrs().HardwareAddr
	i.nodeConfig.Gateway = &Gateway{Name: i.hostGateway, IP: gwIP.IP, MAC: gwMAC}
	gatewayIface.IP = gwIP.IP
	gatewayIface.MAC = gwMAC

	if addrs, err := netlink.AddrList(link, netlink.FAMILY_V4); err != nil {
		klog.Errorf("Failed to query IPv4 address list for interface %s: %v", i.hostGateway, err)
		return err
	} else if addrs != nil {
		for _, addr := range addrs {
			klog.V(4).Infof("Found IPv4 address %s for interface %s", addr.IP.String(), i.hostGateway)
			if addr.IP.Equal(gwAddr.IPNet.IP) {
				klog.V(2).Infof("IPv4 address %s already assigned to interface %s", addr.IP.String(), i.hostGateway)
				return nil
			}
		}
	} else {
		klog.V(2).Infof("Link %s has no configured IPv4 address", i.hostGateway)
	}

	klog.V(2).Infof("Adding address %v to gateway interface %s", gwAddr, i.hostGateway)
	if err := netlink.AddrAdd(link, gwAddr); err != nil {
		klog.Errorf("Failed to set gateway interface %s with address %v: %v", i.hostGateway, gwAddr, err)
		return err
	}
	return nil
}
