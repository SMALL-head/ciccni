// Copyright 2019 Antrea Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ipam

import (
	"ciccni/pkg/apis/cni/pb"
	"fmt"

	"github.com/containernetworking/cni/pkg/invoke"

	current "github.com/containernetworking/cni/pkg/types/100"
)

var ipamDrivers map[string]IPAMDriver

type IPAMConfig struct {
	Type    string `json:"type,omitempty"`
	Subnet  string `json:"subnet,omitempty"`
	Gateway string `json:"gateway,omitempty"`
}

//go:generate mockgen -copyright_file ../../../../hack/boilerplate/license_header.raw.txt -destination testing/mock_ipam.go -package=testing github.com/vmware-tanzu/antrea/pkg/agent/cniserver/ipam IPAMDriver

type IPAMDriver interface {
	Add(args *invoke.Args, networkConfig []byte) (*current.Result, error)
	Del(args *invoke.Args, networkConfig []byte) error
	Check(args *invoke.Args, networkConfig []byte) error
}

func RegisterIPAMDriver(ipamType string, ipamDriver IPAMDriver) error {
	if ipamDrivers == nil {
		ipamDrivers = make(map[string]IPAMDriver)
	}
	if _, existed := ipamDrivers[ipamType]; existed {
		return fmt.Errorf("Already registered IPAM with type %s", ipamType)
	}
	ipamDrivers[ipamType] = ipamDriver
	return nil
}

func argsFromEnv(cniArgs *pb.CniCmdArgs) *invoke.Args {
	return &invoke.Args{
		ContainerID: cniArgs.ContainerId,
		NetNS:       cniArgs.Netns,
		IfName:      cniArgs.Ifname,
		Path:        cniArgs.Path,
	}
}

func ExecIPAMAdd(cniArgs *pb.CniCmdArgs, ipamType string) (*current.Result, error) {
	args := argsFromEnv(cniArgs)
	driver := ipamDrivers[ipamType]
	return driver.Add(args, cniArgs.NetworkConfiguration)
}

func ExecIPAMDelete(cniArgs *pb.CniCmdArgs, ipamType string) error {
	args := argsFromEnv(cniArgs)
	driver := ipamDrivers[ipamType]
	return driver.Del(args, cniArgs.NetworkConfiguration)
}

func ExecIPAMCheck(cniArgs *pb.CniCmdArgs, ipamType string) error {
	args := argsFromEnv(cniArgs)
	driver := ipamDrivers[ipamType]
	return driver.Check(args, cniArgs.NetworkConfiguration)
}

func IsIPAMTypeValid(ipamType string) bool {
	_, valid := ipamDrivers[ipamType]
	return valid
}
