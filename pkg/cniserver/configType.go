package cniserver

import (
	"ciccni/pkg/apis/cni/pb"
	"ciccni/pkg/cniserver/ipam"

	"github.com/containernetworking/cni/pkg/types"
)

type NetworkConfig struct {
	CNIVersion string          `json:"cniVersion,omitempty"`
	Name       string          `json:"name,omitempty"`
	Type       string          `json:"type,omitempty"`
	MTU        int             `json:"mtu,omitempty"`
	DNS        types.DNS       `json:"dns"`
	IPAM       ipam.IPAMConfig `json:"ipam,omitempty"`

	RawPrevResult map[string]interface{} `json:"prevResult,omitempty"`
	PrevResult    types.Result           `json:"-"`
}

type CNIConfig struct {
	*NetworkConfig
	*pb.CniCmdArgs
	*k8sArgs
}
