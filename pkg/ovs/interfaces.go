package ovs

const (
	GENEVE_TUNNEL = "geneve"
	VXLAN_TUNNEL  = "vxlan"

	OVSDatapathSystem = "system"
	OVSDatapathNetdev = "netdev"
)

//go:generate mockgen -copyright_file ../../../hack/boilerplate/license_header.raw.txt -destination testing/mock_ovsconfig.go -package=testing github.com/vmware-tanzu/antrea/pkg/ovs/ovsconfig OVSBridgeClient

type OVSBridgeClient interface {
	Create() Error
	Delete() Error
	GetExternalIDs() (map[string]string, Error)
	SetExternalIDs(externalIDs map[string]interface{}) Error
	CreatePort(name, ifDev string, externalIDs map[string]interface{}) (string, Error)
	CreateGenevePort(name string, ofPortRequest int32, remoteIP string) (string, Error)
	CreateInternalPort(name string, ofPortRequest int32, externalIDs map[string]interface{}) (string, Error)
	CreateVXLANPort(name string, ofPortRequest int32, remoteIP string) (string, Error)
	DeletePort(portUUID string) Error
	DeletePorts(portUUIDList []string) Error
	GetOFPort(ifName string) (int32, Error)
	GetPortData(portUUID, ifName string) (*OVSPortData, Error)
	GetPortList() ([]OVSPortData, Error)
	SetInterfaceMTU(name string, MTU int) error
}
