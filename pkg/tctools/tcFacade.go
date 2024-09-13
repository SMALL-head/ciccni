package tctools

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

func CreateRootHTB(ifName string) error {
	return addHTBToInterface(ifName)
}

func CreateHTBClass(ifName string, parent uint32, classid uint32, rate uint32, burst uint32) error {
	return addHTBClass(ifName, parent, classid, rate, burst)
}

func ConstructTcConfig(k8sClient kubernetes.Interface, podName string, namespace string) (*TCArgs, error) {
	podInfo, err := k8sClient.CoreV1().Pods(namespace).Get(context.TODO(), podName, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("[ConstructTcConfig]-获取pod信息失败, err = %s", err)
		return nil, err
	}
	annotaions := podInfo.ObjectMeta.Annotations
	if annotaions == nil {
		return nil, nil
	}
	res := &TCArgs{}
	if egressRate, ok := annotaions["ciccni/egress-rate"]; ok {
		klog.Infof("[ConstructTcConfig]-[ConstructTcConfig]-解析egress-rate, rate = %s", egressRate)
		rate, err := validateBandwithFormat(egressRate)
		if err != nil {
			klog.Errorf("[ConstructTcConfig]-[validateBandwithFormat]-解析egress-rate失败, err = %s", err)
			return nil, err
		}
		res.Rate = uint32(rate)
		res.Burst = uint32(rate) / 10
	}
	return res, nil
}
