package main

import (
	"github.com/openshift/ptp-operator/test/utils/client"
	"github.com/test-network-function/l2discovery/l2lib"
)

func main() {
	client.Client = client.New("")
	l2lib.SetL2Client(client.Client, client.Client.Config, client.Client.PtpV1Interface)
	_ = l2lib.GlobalL2DiscoveryConfig.DiscoverL2Connectivity()
}
