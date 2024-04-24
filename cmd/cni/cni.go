package main

import (
	"ciccni/pkg/cni"
	"github.com/containernetworking/cni/pkg/skel"
	cniversion "github.com/containernetworking/cni/pkg/version"
)

func main() {
	skel.PluginMain(
		cni.ActionAdd.Request,
		cni.ActionCheck.Request,
		cni.ActionDel.Request,
		cniversion.PluginSupports("0.3.0"),
		"cic-cni")
}
