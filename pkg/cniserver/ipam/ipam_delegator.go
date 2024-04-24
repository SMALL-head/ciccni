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
	"context"
	"os"
	"path/filepath"

	"github.com/containernetworking/cni/pkg/invoke"

	current "github.com/containernetworking/cni/pkg/types/100"

	"k8s.io/klog"
)

const (
	IPAM_HOST_LOCAL = "host-local"
)

type IPAMDelegator struct {
	pluginType string
}

func (d *IPAMDelegator) Add(args *invoke.Args, networkConfig []byte) (*current.Result, error) {
	var success = false
	defer func() {
		if !success {
			// Rollback to delete assigned network configuration for failed to execute Add operation
			args.Command = "DEL"
			if err := delegateNoResult(d.pluginType, networkConfig, args); err != nil {
				klog.Errorf("Failed to roll back to delete configuration %s, %v", string(networkConfig), err)
			}
		}
	}()
	args.Command = "ADD"
	r, err := delegateWithResult(d.pluginType, networkConfig, args)
	if err != nil {
		return nil, err
	}

	success = true
	return r, nil
}

func (d *IPAMDelegator) Del(args *invoke.Args, networkConfig []byte) error {
	args.Command = "DEL"
	if err := delegateNoResult(d.pluginType, networkConfig, args); err != nil {
		return err
	}

	return nil
}

func (d *IPAMDelegator) Check(args *invoke.Args, networkConfig []byte) error {
	args.Command = "CHECK"
	if err := delegateNoResult(d.pluginType, networkConfig, args); err != nil {
		return err
	}
	return nil
}

var defaultExec = &invoke.DefaultExec{
	RawExec: &invoke.RawExec{Stderr: os.Stderr},
}

func delegateCommon(delegatePlugin string, exec invoke.Exec, cniPath string) (string, invoke.Exec, error) {
	paths := filepath.SplitList(cniPath)
	pluginPath, err := exec.FindInPath(delegatePlugin, paths)
	if err != nil {
		return "", nil, err
	}

	return pluginPath, exec, nil
}

func delegateWithResult(delegatePlugin string, networkConfig []byte, args *invoke.Args) (*current.Result, error) {
	ctx := context.TODO()
	pluginPath, realExec, err := delegateCommon(delegatePlugin, defaultExec, args.Path)
	if err != nil {
		return nil, err
	}

	res, err := invoke.ExecPluginWithResult(ctx, pluginPath, networkConfig, args, realExec)
	res040, err := current.NewResultFromResult(res)
	return res040, err
}

func delegateNoResult(delegatePlugin string, networkConfig []byte, args *invoke.Args) error {
	ctx := context.TODO()
	pluginPath, realExec, err := delegateCommon(delegatePlugin, defaultExec, args.Path)
	if err != nil {
		return err
	}

	return invoke.ExecPluginWithoutResult(ctx, pluginPath, networkConfig, args, realExec)
}

func init() {
	if err := RegisterIPAMDriver(IPAM_HOST_LOCAL, &IPAMDelegator{pluginType: IPAM_HOST_LOCAL}); err != nil {
		klog.Errorf("Failed to register IPAM plugin on type %s", IPAM_HOST_LOCAL)
	}
}
