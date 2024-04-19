package k8sclient

import (
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"
)

// CreateClient 创建一个k8s的Clientset
func CreateClient() (clientset.Interface, error) {
	var kubeConfig *rest.Config
	kubeConfig, err := clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
	if err != nil {
		klog.Infof("[k8s-client/client.go]-[CreateClient]-未找到.kube/config文件，使用InClusterConfig")
		kubeConfig, err = rest.InClusterConfig()
	}
	if err != nil {
		klog.Errorf("[k8s-client/client.go]-[CreateClient]-使用Incluster配置文件失败")
		return nil, err
	}

	client, err  := clientset.NewForConfig(kubeConfig)
	if err != nil {
		klog.Errorf("[client.go]-[CreateClient]-使用config文件创建失败")
		return nil, err
	}
	return client, err

}