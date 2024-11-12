package e2enode

import (
	"context"
	"time"

	imageutils "k8s.io/kubernetes/test/utils/image"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	kubeletconfig "k8s.io/kubernetes/pkg/kubelet/apis/config"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/kubernetes/test/e2e/feature"
	"k8s.io/kubernetes/test/e2e/framework"
	e2epod "k8s.io/kubernetes/test/e2e/framework/pod"
	admissionapi "k8s.io/pod-security-admission/api"
)

const containerName = "restarts"

var _ = SIGDescribe("Container Restart", feature.CriProxy, framework.WithSerial(), func() {
	f := framework.NewDefaultFramework("container-restart")
	f.NamespacePodSecurityLevel = admissionapi.LevelPrivileged

	ginkgo.Context("Container restart backs off", func() {

		ginkgo.BeforeEach(func() {
			if err := resetCRIProxyInjector(e2eCriProxy); err != nil {
				ginkgo.Skip("Skip the test since the CRI Proxy is undefined.")
			}
		})

		ginkgo.AfterEach(func() {
			err := resetCRIProxyInjector(e2eCriProxy)
			framework.ExpectNoError(err)
		})

		ginkgo.It("Container restart backs off.", func(ctx context.Context) {
			// 0s, 0s, 10s, 30s, 70s, 150s, 310s
			doTest(ctx, f, 5, containerName, 7)
		})
	})

	ginkgo.Context("Alternate container restart backs off as expected", func() {

		tempSetCurrentKubeletConfig(f, func(ctx context.Context, initialConfig *kubeletconfig.KubeletConfiguration) {
			initialConfig.CrashLoopBackOff.MaxContainerRestartPeriod = &metav1.Duration{Duration: time.Duration(30 * time.Second)}
			initialConfig.FeatureGates = map[string]bool{"KubeletCrashLoopBackOffMax": true}
		})

		ginkgo.BeforeEach(func() {
			if err := resetCRIProxyInjector(e2eCriProxy); err != nil {
				ginkgo.Skip("Skip the test since the CRI Proxy is undefined.")
			}
		})

		ginkgo.AfterEach(func() {
			err := resetCRIProxyInjector(e2eCriProxy)
			framework.ExpectNoError(err)
		})

		ginkgo.It("Alternate restart backs off.", func(ctx context.Context) {
			// 0s, 0s, 10s, 30s, 60s, 90s, 120s, 150, 180, 210)
			doTest(ctx, f, 7, containerName, 10)
		})
	})
})

func doTest(ctx context.Context, f *framework.Framework, targetRestarts int, containerName string, maxRestarts int) {

	pod := e2epod.NewPodClient(f).Create(ctx, newFailAlwaysPod())
	podErr := e2epod.WaitForPodContainerToFail(ctx, f.ClientSet, f.Namespace.Name, pod.Name, 0, "CrashLoopBackOff", 1*time.Minute)
	gomega.Expect(podErr).To(gomega.HaveOccurred())

	// Wait for 150s worth of backoffs to occur so we can confirm the backoff growth.
	podErr = e2epod.WaitForContainerRestartedNTimes(ctx, f.ClientSet, f.Namespace.Name, pod.Name, "restart", 150*time.Second, targetRestarts)
	gomega.Expect(podErr).ShouldNot(gomega.HaveOccurred(), "Expected container to repeatedly back off container failures")

	r, err := extractObservedBackoff(ctx, f, pod.Name, containerName)
	framework.ExpectNoError(err)

	gomega.Expect(r).Should(gomega.BeNumerically("<=", maxRestarts))
}

func extractObservedBackoff(ctx context.Context, f *framework.Framework, podName string, containerName string) (int32, error) {
	var r int32
	pod, err := f.ClientSet.CoreV1().Pods(f.Namespace.Name).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return r, err
	}
	for _, statuses := range [][]v1.ContainerStatus{pod.Status.ContainerStatuses, pod.Status.InitContainerStatuses, pod.Status.EphemeralContainerStatuses} {
		for _, cs := range statuses {
			if cs.Name == containerName {
				return r, nil
			}
		}
	}
	return r, nil
}

func newFailAlwaysPod() *v1.Pod {
	podName := "container-restart" + string(uuid.NewUUID())
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: podName,
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:            containerName,
					Image:           imageutils.GetBusyBoxImageName(),
					ImagePullPolicy: v1.PullIfNotPresent,
				},
			},
		},
	}
	return pod
}
