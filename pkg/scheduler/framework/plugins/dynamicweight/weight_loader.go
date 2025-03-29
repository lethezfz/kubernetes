// pkg/scheduler/framework/plugins/dynamicweight/weight_loader.go
package dynamicweight

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	//"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	//corev1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

const (
	configMapNamespace = "kube-system"
	configMapName      = "dynamic-weight-config"
)

// 配置加载器接口定义
// 作用：提供获取最新权重配置的能力
type WeightLoader interface {
	GetWeights() *DynamicWeightArgs
}

// 配置加载器实现：从ConfigMap读取并监听变更
type weightLoader struct {
	client kubernetes.Interface // Kubernetes API客户端
	args   *DynamicWeightArgs   // 当前生效的配置
	lock   sync.RWMutex         // 读写锁（保障线程安全）
	//controller cache.Controller
}

// 创建配置加载器实例
func NewWeightLoader(client kubernetes.Interface) (WeightLoader, error) {
	wl := &weightLoader{
		client: client,
		args:   &DynamicWeightArgs{},
	}

	// 初始加载配置
	if err := wl.loadConfig(); err != nil {
		return nil, fmt.Errorf("failed to load initial config: %v", err)
	}

	// 启动后台监听协程
	go wl.startInformer()

	return wl, nil
}

// 获取当前配置（线程安全）
func (wl *weightLoader) GetWeights() *DynamicWeightArgs {
	wl.lock.RLock()           // 加读锁（允许多个读取）
	defer wl.lock.RUnlock()   // 函数结束时释放锁
	return wl.args.DeepCopy() // 返回配置副本
}

// 加载配置的完整流程
func (wl *weightLoader) loadConfig() error {
	// 从Kubernetes API获取ConfigMap
	cm, err := wl.client.CoreV1().ConfigMaps(configMapNamespace).Get(
		context.Background(),
		configMapName,
		metav1.GetOptions{},
	)
	if err != nil {
		return fmt.Errorf("error fetching ConfigMap %s/%s: %v",
			configMapNamespace, configMapName, err)
	}

	// 解析JSON配置数据
	configData, ok := cm.Data["config.json"]
	if !ok {
		return fmt.Errorf("ConfigMap %s/%s missing 'config.json' key",
			configMapNamespace, configMapName)
	}

	newArgs := &DynamicWeightArgs{}
	if err := json.Unmarshal([]byte(configData), newArgs); err != nil {
		return fmt.Errorf("error unmarshaling config: %v", err)
	}

	// 更新配置（加写锁）
	wl.lock.Lock()
	defer wl.lock.Unlock()
	wl.args = newArgs
	return nil
}

// 启动监听ConfigMap变更的后台协程
func (wl *weightLoader) startInformer() {
	// 创建Kubernetes Informer（监听指定ConfigMap）
	factory := informers.NewSharedInformerFactoryWithOptions(
		wl.client,
		5*time.Minute, // 每5分钟全量同步一次
		informers.WithNamespace(configMapNamespace),
		informers.WithTweakListOptions(func(options *metav1.ListOptions) {
			options.FieldSelector = "metadata.name=" + configMapName
		}),
	)

	// 注册变更回调
	cmInformer := factory.Core().V1().ConfigMaps()
	cmInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			// 当ConfigMap更新时重新加载配置
			oldCM := oldObj.(*v1.ConfigMap)
			newCM := newObj.(*v1.ConfigMap)
			if oldCM.ResourceVersion == newCM.ResourceVersion {
				return
			}
			klog.InfoS("ConfigMap updated, reloading weights")
			if err := wl.loadConfig(); err != nil {
				klog.ErrorS(err, "Failed to reload config")
			}
		},
	})

	// 启动监听
	stopCh := make(chan struct{})
	defer close(stopCh)
	factory.Start(stopCh)
	factory.WaitForCacheSync(stopCh)

	<-stopCh // 阻塞直到进程退出
}

// DeepCopy 用于线程安全获取配置副本
func (args *DynamicWeightArgs) DeepCopy() *DynamicWeightArgs {
	newArgs := &DynamicWeightArgs{
		DefaultWeights: make(map[string]float64),
		LabelWeights:   make(map[string]map[string]float64),
	}

	for k, v := range args.DefaultWeights {
		newArgs.DefaultWeights[k] = v
	}

	for label, weights := range args.LabelWeights {
		newWeights := make(map[string]float64)
		for res, w := range weights {
			newWeights[res] = w
		}
		newArgs.LabelWeights[label] = newWeights
	}

	return newArgs
}
