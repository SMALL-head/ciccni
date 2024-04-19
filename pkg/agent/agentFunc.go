package agent

import (
	"ciccni/pkg/openflow"
	"ciccni/pkg/ovs"
	"context"
	"fmt"
	"net"
	"os"

	v1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

const (
	
	NodeNameEnvKey = "NODE_NAME"
	TunPortName = "tun0"
	tunOFPort = 1
)

type NodeConfig struct {
	NodeName string
	PodCIDR *net.IPNet
	Bridge string

}

type Initializer struct {
	k8sClient kubernetes.Interface
	nodeConfig *NodeConfig
	ovsBridgeClient ovs.OVSBridgeClient
	ifaceStore InterfaceStore
	ofClient openflow.Client
} 

func NewInitializer(k8sClient kubernetes.Interface, ovsBridgeClient ovs.OVSBridgeClient, ifaceStore InterfaceStore, ofCLient openflow.Client) *Initializer{
	return &Initializer{
		k8sClient: k8sClient,
		ovsBridgeClient: ovsBridgeClient,
		ifaceStore: ifaceStore,
		ofClient: ofCLient,
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

	// 2. 初始化网桥
	klog.Infof("[agentFunc.go]-[Initialize]-初始化网桥")
	if err := i.setUpOVSBridge(); err != nil {
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
	// gatewayIP := ip.NextIP(localSubnet.IP.Mask(localSubnet.Mask))
	i.nodeConfig = &NodeConfig{NodeName: nodeName, PodCIDR: localSubnet,}
	return nil
}

func (i *Initializer) GetNodeConfig() *NodeConfig {
	return i.nodeConfig
}

func (i *Initializer) setUpOVSBridge() error {
	// 1. 创建网桥
	if err := i.ovsBridgeClient.Create() ; err != nil {
		klog.Errorf("[agentFunc.go]-[setUpOVSBridge]-创建网桥失败, err=%v", err)
		return err
	}

	// 2. 构造ovs端口cache
	if err := i.ifaceStore.Initialize(i.ovsBridgeClient, TunPortName); err != nil {
		klog.Errorf("[agentFunc.go]-[setUpOVSBridge]-无法初始化ifaceStore, err=%v", err)
		return err
	}

	// 3. 创建ovs对应的tunnel端口
	if err := i.setUpTunnelInterface(TunPortName); err != nil {
		return err
	}

	// 4. 写入基本的openflow流表项
	if err := i.initOpenFlow(); err != nil {
		return err
	}
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
			i.ofClient.InstallLocalIPFlow(node.Name, i.nodeConfig.PodCIDR.String())
		} else {
			var nodeAddress net.IP
			for _, address := range node.Status.Addresses {
				if address.Type == v1.NodeInternalIP {
					nodeAddress = net.ParseIP(address.Address)
					break
				}
			}
			if nodeAddress != nil {
				i.ofClient.InstallTunFlow(i.nodeConfig.PodCIDR.String(), 0, nodeAddress)
			} else {
				klog.Errorf("[agentFunc.go]-[constructIPTunFlow]-为node:%s安装ip流表规则出错", node.Name)
			}
		}
	}
}

// getNodeName 尝试通过环境变量获取nodeName。注意，这个环境变量应该通过yaml文件中进行配置
func getNodeName() (string, error) {
	nodeName := os.Getenv(NodeNameEnvKey)
	if nodeName != "" {
		return nodeName, nil
	}
	klog.Infof("[agentFunc.go]-[getNodeName]-无法通过%s获取nodeName，尝试使用hostname代替", NodeNameEnvKey)
	hostname, err := os.Hostname()
	if err != nil {
		return "", err
	}
	return hostname, nil
	
}

