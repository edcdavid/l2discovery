package l2lib

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	ptpv1 "github.com/openshift/ptp-operator/pkg/client/clientset/versioned/typed/ptp/v1"
	"github.com/openshift/ptp-operator/test/utils/daemonsets"
	"github.com/sirupsen/logrus"
	l2 "github.com/test-network-function/l2discovery/export"
	"github.com/test-network-function/l2discovery/l2lib/pkg/l2client"
	"github.com/test-network-function/l2discovery/l2lib/pkg/pods"

	"github.com/yourbasic/graph"
	v1core "k8s.io/api/core/v1"
)

func init() {
	GlobalL2DiscoveryConfig.refresh = true
}

func SetL2Client(k8sClient kubernetes.Interface, restClient *rest.Config, ptpClient ptpv1.PtpV1Interface) {
	l2client.Set(k8sClient, restClient, ptpClient)
}

const (
	ExperimentalEthertype          = "88b5"
	PtpEthertype                   = "88f7"
	LocalInterfaces                = "0000"
	L2DaemonsetManagedString       = "MANAGED"
	L2DaemonsetPreConfiguredString = "PRECONFIGURED"
	L2DiscoveryDsName              = "l2discovery"
	L2DiscoveryNsName              = "default"
	L2DiscoveryContainerName       = "l2discovery"
	timeoutDaemon                  = time.Second * 60
	L2DiscoveryDuration            = time.Second * 15
	l2DiscoveryImage               = "quay.io/deliedit/l2discovery654419616:david"
)

type L2DaemonsetMode int64

const (
	// In managed mode, the L2 Topology discovery Daemonset is created by the conformance suite
	Managed L2DaemonsetMode = iota
	// In pre-configured mode, the L2 topology daemonset is pre-configured by the user in the cluster
	PreConfigured
)

func (mode L2DaemonsetMode) String() string {
	switch mode {
	case Managed:
		return L2DaemonsetManagedString
	case PreConfigured:
		return L2DaemonsetPreConfiguredString
	default:
		return L2DaemonsetManagedString
	}
}

func StringToL2Mode(aString string) L2DaemonsetMode {
	switch aString {
	case L2DaemonsetManagedString:
		return Managed
	case L2DaemonsetPreConfiguredString:
		return PreConfigured
	default:
		return Managed
	}
}

var GlobalL2DiscoveryConfig L2DiscoveryConfig

// Object used to index interfaces in a cluster
type IfClusterIndex struct {
	// interface name
	IfName string
	// node name
	NodeName string
}

// Object representing a ptp interface within a cluster.
type PtpIf struct {
	// Mac address of the Ethernet interface
	MacAddress string
	// Index of the interface in the cluster (node/interface name)
	IfClusterIndex
	// PCI address
	IfPci l2.PCIAddress
}

type L2DiscoveryConfig struct {
	// Map of L2 topology as discovered by L2 discovery mechanism
	DiscoveryMap map[string]map[string]map[string]*l2.Neighbors
	// L2 topology graph created from discovery map. This is the main internal graph
	L2ConnectivityMap *graph.Mutable
	// Max size of graph
	MaxL2GraphSize int
	// list of cluster interfaces indexed with a simple integer (X) for readability in the graph
	PtpIfList []*PtpIf
	// list of L2discovery daemonset pods
	L2DiscoveryPods map[string]*v1core.Pod
	// Mapping between clusterwide interface index and Mac address
	ClusterMacs map[IfClusterIndex]string
	// Mapping between clusterwide interface index and a simple integer (X) for readability in the graph
	ClusterIndexToInt map[IfClusterIndex]int
	// Mapping between a cluster wide MAC address and a simple integer (X) for readability in the graph
	ClusterMacToInt map[string]int
	// Mapping between a Mac address and a cluster wide interface index
	ClusterIndexes map[string]IfClusterIndex
	// 2D Map holding the valid ptp interfaces as reported by the ptp-operator api. map[ <node name>]map[<interface name>]
	ptpInterfaces map[string]map[string]bool
	// indicates whether the L2discovery daemonset is created by the test suite (managed) or not
	L2DsMode L2DaemonsetMode
	// LANs identified in the graph
	LANs *[][]int
	// List of port receiving PTP frames (assuming valid GM signal received)
	PortsGettingPTP []*PtpIf
	// interfaces to avoid when running the tests
	SkippedInterfaces []string
	// Indicates that the L2 configuration must be refreshed
	refresh bool
}

func (index IfClusterIndex) String() string {
	return fmt.Sprintf("%s_%s", index.NodeName, index.IfName)
}

func (iface *PtpIf) String() string {
	return fmt.Sprintf("%s : %s", iface.NodeName, iface.IfName)
}

func (iface *PtpIf) String1() string {
	return fmt.Sprintf("index:%s mac:%s", iface.IfClusterIndex, iface.MacAddress)
}

// Gets existing L2 configuration or creates a new one  (if refresh is set to true)
func GetL2DiscoveryConfig() (config *L2DiscoveryConfig, err error) {
	if GlobalL2DiscoveryConfig.refresh {
		err := GlobalL2DiscoveryConfig.DiscoverL2Connectivity()
		if err != nil {
			GlobalL2DiscoveryConfig.refresh = false
			return config, fmt.Errorf("could not get L2 config")
		}
	}
	GlobalL2DiscoveryConfig.refresh = false
	return &GlobalL2DiscoveryConfig, nil
}

// Resets the L2 configuration
func (config *L2DiscoveryConfig) reset() {
	GlobalL2DiscoveryConfig.PtpIfList = []*PtpIf{}
	GlobalL2DiscoveryConfig.L2DiscoveryPods = make(map[string]*v1core.Pod)
	GlobalL2DiscoveryConfig.ClusterMacs = make(map[IfClusterIndex]string)
	GlobalL2DiscoveryConfig.ClusterIndexes = make(map[string]IfClusterIndex)
	GlobalL2DiscoveryConfig.ClusterMacToInt = make(map[string]int)
	GlobalL2DiscoveryConfig.ClusterIndexToInt = make(map[IfClusterIndex]int)
	GlobalL2DiscoveryConfig.ClusterIndexes = make(map[string]IfClusterIndex)
}

// Discovers the L2 connectivity using l2discovery daemonset
func (config *L2DiscoveryConfig) DiscoverL2Connectivity() error {
	GlobalL2DiscoveryConfig.reset()

	// initializes clusterwide ptp interfaces
	var err error
	// Create L2 discovery daemonset
	config.L2DsMode = StringToL2Mode(os.Getenv("L2_DAEMONSET"))
	if config.L2DsMode == Managed {
		_, err = daemonsets.CreateDaemonSet(L2DiscoveryDsName, L2DiscoveryNsName, L2DiscoveryContainerName, l2DiscoveryImage, timeoutDaemon)
		if err != nil {
			logrus.Errorf("error creating l2 discovery daemonset, err=%s", err)
		}
	}
	// Sleep a short time to allow discovery to happen (first report after 5s)
	time.Sleep(L2DiscoveryDuration)
	// Get the L2 topology pods
	err = GlobalL2DiscoveryConfig.getL2TopologyDiscoveryPods()
	if err != nil {
		return fmt.Errorf("could not get l2 discovery pods, err=%s", err)
	}
	err = config.getL2Disc()
	if err != nil {
		logrus.Errorf("error getting l2 discovery data, err=%s", err)
	}
	// Delete L2 discovery daemonset
	if config.L2DsMode == Managed {
		err = daemonsets.DeleteDaemonSet(L2DiscoveryDsName, L2DiscoveryNsName)
		if err != nil {
			logrus.Errorf("error deleting l2 discovery daemonset, err=%s", err)
		}
	}
	// Create a graph from the discovered data
	err = config.createL2InternalGraph()
	if err != nil {
		return err
	}
	return nil
}

// Print database with all NICs
func (config *L2DiscoveryConfig) PrintAllNICs() {
	for index, aIf := range config.PtpIfList {
		logrus.Infof("%d %s", index, aIf)
	}

	for index, island := range *config.LANs {
		aLog := fmt.Sprintf("island %d: ", index)
		for _, aIf := range island {
			aLog += fmt.Sprintf("%s **** ", config.PtpIfList[aIf])
		}
		logrus.Info(aLog)
	}
}

// Gets the latest topology reports from the l2discovery pods
func (config *L2DiscoveryConfig) getL2Disc() error {
	config.DiscoveryMap = make(map[string]map[string]map[string]*l2.Neighbors)
	index := 0
	for _, aPod := range config.L2DiscoveryPods {
		podLogs, _ := pods.GetLog(aPod, aPod.Spec.Containers[0].Name)
		indexReport := strings.LastIndex(podLogs, "JSON_REPORT")
		report := strings.Split(strings.Split(podLogs[indexReport:], `\n`)[0], "JSON_REPORT")[1]
		var discDataPerNode map[string]map[string]*l2.Neighbors
		if err := json.Unmarshal([]byte(report), &discDataPerNode); err != nil {
			return err
		}

		if _, ok := config.DiscoveryMap[aPod.Spec.NodeName]; !ok {
			config.DiscoveryMap[aPod.Spec.NodeName] = make(map[string]map[string]*l2.Neighbors)
		}
		config.DiscoveryMap[aPod.Spec.NodeName] = discDataPerNode

		config.createMaps(discDataPerNode, aPod.Spec.NodeName, &index)
	}
	config.MaxL2GraphSize = index
	return nil
}

// Creates the Main topology graph
func (config *L2DiscoveryConfig) createL2InternalGraph() error {
	GlobalL2DiscoveryConfig.L2ConnectivityMap = graph.New(config.MaxL2GraphSize)
	for _, aPod := range config.L2DiscoveryPods {
		for iface, ifaceMap := range config.DiscoveryMap[aPod.Spec.NodeName][ExperimentalEthertype] {
			for mac := range ifaceMap.Remote {
				v := config.ClusterIndexToInt[IfClusterIndex{IfName: iface, NodeName: aPod.Spec.NodeName}]
				w := config.ClusterMacToInt[mac]

				if _, ok := config.ptpInterfaces[config.PtpIfList[v].NodeName][config.PtpIfList[v].IfName]; ok {
					if _, ok := config.ptpInterfaces[config.PtpIfList[w].NodeName][config.PtpIfList[w].IfName]; ok {
						// only add ptp capable interfaces
						config.L2ConnectivityMap.AddBoth(v, w)
					}
				}
			}
		}
	}
	// Init LANs
	out := graph.Components(config.L2ConnectivityMap)
	logrus.Infof("%v", out)
	config.LANs = &out
	config.PrintAllNICs()

	logrus.Infof("NIC num: %d", config.MaxL2GraphSize)
	return nil
}

// Gets the grandmaster port by using L2 discovery data for ptp ethertype
func (config *L2DiscoveryConfig) getInterfacesReceivingPTP() {
	for _, aPod := range config.L2DiscoveryPods {
		for _, ifaceMap := range config.DiscoveryMap[aPod.Spec.NodeName][PtpEthertype] {
			if len(ifaceMap.Remote) == 0 {
				continue
			}
			aPortGettingPTP := &PtpIf{}
			aPortGettingPTP.IfName = ifaceMap.Local.IfName
			aPortGettingPTP.NodeName = aPod.Spec.NodeName
			config.PortsGettingPTP = append(config.PortsGettingPTP, aPortGettingPTP)
		}
	}
	logrus.Infof("interfaces receiving PTP frames: %v", config.PortsGettingPTP)
}

// Creates Mapping tables between interfaces index, mac address, and graph integer indexes
func (config *L2DiscoveryConfig) createMaps(disc map[string]map[string]*l2.Neighbors, nodeName string, index *int) {
	config.updateMaps(disc, nodeName, index, ExperimentalEthertype)
	config.updateMaps(disc, nodeName, index, LocalInterfaces)
	config.getInterfacesReceivingPTP()
}

// updates Mapping tables between interfaces index, mac address, and graph integer indexes for a given ethertype
func (config *L2DiscoveryConfig) updateMaps(disc map[string]map[string]*l2.Neighbors, nodeName string, index *int, ethertype string) {
	for _, ifaceData := range disc[ethertype] {
		if _, ok := config.ClusterMacToInt[ifaceData.Local.IfMac.Data]; ok {
			continue
		}
		config.ClusterMacToInt[ifaceData.Local.IfMac.Data] = *index
		config.ClusterIndexToInt[IfClusterIndex{IfName: ifaceData.Local.IfName, NodeName: nodeName}] = *index
		config.ClusterMacs[IfClusterIndex{IfName: ifaceData.Local.IfName, NodeName: nodeName}] = ifaceData.Local.IfMac.Data
		config.ClusterIndexes[ifaceData.Local.IfMac.Data] = IfClusterIndex{IfName: ifaceData.Local.IfName, NodeName: nodeName}
		aInterface := PtpIf{}
		aInterface.NodeName = nodeName
		aInterface.IfName = ifaceData.Local.IfName
		aInterface.MacAddress = ifaceData.Local.IfMac.Data
		aInterface.IfPci = ifaceData.Local.IfPci
		config.PtpIfList = append(config.PtpIfList, &aInterface)
		(*index)++
	}
}

// Gets the list of l2discovery pods
func (config *L2DiscoveryConfig) getL2TopologyDiscoveryPods() error {
	aPodList, err := l2client.Client.K8sClient.CoreV1().Pods(L2DiscoveryNsName).List(context.Background(), metav1.ListOptions{LabelSelector: "name=l2discovery"})
	if err != nil {
		return fmt.Errorf("could not get list of linkloop pods, err=%s", err)
	}
	for index := range aPodList.Items {
		config.L2DiscoveryPods[aPodList.Items[index].Spec.NodeName] = &aPodList.Items[index]
	}
	return nil
}
