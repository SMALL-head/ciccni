package cni

import (
	"ciccni/pkg/cni"
	"github.com/containernetworking/cni/pkg/skel"
	cniversion "github.com/containernetworking/cni/pkg/version"
)

func main() {
	skel.PluginMain(
		cni.CmdAdd,
		cni.CmdCheck,
		cni.CmdDel,
		cniversion.PluginSupports("0.3.0"),
		"cic-cni")
}
