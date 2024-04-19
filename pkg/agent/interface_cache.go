package agent

import (
	"ciccni/pkg/ovs"
	"sync"

	"net"

	"k8s.io/klog"
)

type InterfaceType uint8

const (
	ContainerInterface InterfaceType = iota
	TunnelInterface 
)

const (
	OVSExternalIDMAC          = "attached-mac"
	OVSExternalIDIP           = "ip-address"
	OVSExternalIDContainerID  = "container-id"
	OVSExternalIDPodName      = "pod-name"
	OVSExternalIDPodNamespace = "pod-namespace"
)

type OVSPortConfig struct {
	IfaceName string
	PortUUID string
	OFPort int32
}

type InterfaceConfig struct {
	ID string
	Type InterfaceType
	IP net.IP
	MAC net.HardwareAddr
	PodName string
	PodNamespace string
	NetNS string
	*OVSPortConfig
}

func NewTunnelInterface(tunnelName string) *InterfaceConfig {
	return &InterfaceConfig{ID: tunnelName, Type: TunnelInterface}
}

// InterfaceStore 缓存接口，支持add/delete/get操作
type InterfaceStore interface {
	Initialize(ovsBridgeClient ovs.OVSBridgeClient, tunnelPort string) error

	// AddInterface 对应的key值一般选用{port.Name}
	AddInterface(ifaceID string, interfaceConfig *InterfaceConfig)
	DeleteInterface(ifaceID string)
	GetInterface(ifaceID string) (*InterfaceConfig, bool)
	// GetContainerInterface(podName string, podNamespace string) (*InterfaceConfig, bool)
	GetContainerInterfaceNum() int
	Len() int
	GetInterfaceIDs() []string
}

type interfaceCache struct {
	sync.RWMutex
	cache map[string]*InterfaceConfig
}

func (i *interfaceCache) Initialize(ovsBridgeClient ovs.OVSBridgeClient, tunnelPort string) error {
	ovsPorts, err := ovsBridgeClient.GetPortList()
	if err != nil {
		klog.Errorf("[interface_cache.go]-[Initialize]-无法list OVS ports, err=%v", err)
		return err
	}

	for _, port := range ovsPorts {
		portcfg := &OVSPortConfig{IfaceName: port.Name, PortUUID: port.UUID, OFPort: port.OFPort}
		var interfaceConfig *InterfaceConfig
		switch {
		case port.Name == tunnelPort:
			interfaceConfig = &InterfaceConfig{Type: TunnelInterface, OVSPortConfig: portcfg, ID: tunnelPort}
		default:
			if port.ExternalIDs == nil {
				klog.V(2).Infof("[interface_cache.go]-[Initialize]- OVSport %s 没有external_ids", port.Name)
				continue
			}
			// ovs port中也许会有对应的容器内部的port。但是目前先不写这个逻辑，后续有机会再加入
		}

		// 对于每一个port都做一次缓存
		if interfaceConfig != nil {
			i.cache[interfaceConfig.IfaceName] = interfaceConfig
		}
	}
	return nil
}

func (i *interfaceCache) AddInterface(ifaceID string, interfaceConfig *InterfaceConfig) {
	i.Lock()
	defer i.Unlock()
	i.cache[ifaceID] = interfaceConfig
}

func (i *interfaceCache) DeleteInterface(ifaceID string) {
	i.Lock()
	defer i.Unlock()
	delete(i.cache, ifaceID)
}

func (i *interfaceCache) GetInterface(ifaceID string) (*InterfaceConfig, bool) {
	i.RLock()
	defer i.RUnlock()
	iface, found := i.cache[ifaceID]
	return iface, found
}

func (i *interfaceCache) GetContainerInterfaceNum() int {
	num := 0
	i.RLock()
	defer i.RUnlock()
	for _, v := range i.cache {
		if v.Type == ContainerInterface {
			num++
		}
	}
	return num
}

func (i *interfaceCache) Len() int {
	i.RLock()
	defer i.RUnlock()
	return len(i.cache)
}

func (i *interfaceCache) GetInterfaceIDs() []string {
	i.RLock()
	defer i.RUnlock()
	ids := make([]string, 0, len(i.cache))
	for id := range i.cache {
		ids = append(ids, id)
	}
	return ids
}

func NewInterfaceStore() InterfaceStore {
	return &interfaceCache{cache: make(map[string]*InterfaceConfig)}
}

// NewContainerInterfaceConfig creates container interface configuration
func NewContainerInterfaceConfig(containerID string, podName string, podNamespace string, containerNetNS string, mac net.HardwareAddr, ip net.IP) *InterfaceConfig {
	containerConfig := &InterfaceConfig{ID: containerID, PodName: podName, PodNamespace: podNamespace, NetNS: containerNetNS, MAC: mac, IP: ip, Type: ContainerInterface}
	return containerConfig
}

// BuildOVSPortExternalIDs parses OVS port external_ids from local cache, it is used to check container configuration
func BuildOVSPortExternalIDs(containerConfig *InterfaceConfig) map[string]interface{} {
	externalIDs := make(map[string]interface{})
	externalIDs[OVSExternalIDMAC] = containerConfig.MAC.String()
	externalIDs[OVSExternalIDContainerID] = containerConfig.ID
	externalIDs[OVSExternalIDIP] = containerConfig.IP.String()
	externalIDs[OVSExternalIDPodName] = containerConfig.PodName
	externalIDs[OVSExternalIDPodNamespace] = containerConfig.PodNamespace
	return externalIDs
}


