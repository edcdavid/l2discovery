package l2client

import (
	ptpv1 "github.com/openshift/ptp-operator/pkg/client/clientset/versioned/typed/ptp/v1"
	daemonsets "github.com/test-network-function/privileged-daemonset"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type L2Client struct {
	K8sClient kubernetes.Interface
	Rest      *rest.Config
	Ptp       ptpv1.PtpV1Interface
}

var Client = L2Client{}

func Set(k8sClient kubernetes.Interface, restClient *rest.Config, ptpClient ptpv1.PtpV1Interface) {
	Client.K8sClient = k8sClient
	Client.Rest = restClient
	Client.Ptp = ptpClient
	daemonsets.SetDaemonSetClient(Client.K8sClient)
}
