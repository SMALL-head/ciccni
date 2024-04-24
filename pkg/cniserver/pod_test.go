package cniserver_test

import (
	"testing"

	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/stretchr/testify/require"
)

func TestPodVethCreate(t *testing.T) {
	ns1Path := "/var/run/netns/ns1"
	ns1, err := ns.GetNS(ns1Path)
	ifName := "eth0"
	hostVethName := "hostVeth"
	MTU := 1500
	require.NoError(t, err)
	err = ns1.Do(func(hostNS ns.NetNS) error {
		_, _, err := ip.SetupVethWithName(ifName, hostVethName, MTU, "", hostNS)
		if err != nil {
			return err
		}

		return nil
	})
	require.NoError(t, err)


}