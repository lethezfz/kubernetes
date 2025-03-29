// pkg/scheduler/framework/plugins/dynamicweight/plugin.go
package dynamicweight

import (
	"context"
	"fmt"
	"github.com/prometheus/common/model"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/names"
	//"strings"
	//"sync"
	"time"

	prometheus "github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	//"k8s.io/kubernetes/pkg/scheduler/framework/plugins/feature"
)

const (
	// 定义插件名称常量
	Name = names.DynamicWeight
)

// DynamicWeight 插件结构体必须实现framework.Plugin接口
type DynamicWeight struct {
	handle       framework.Handle // 调度器上下文，提供集群状态访问
	weightLoader WeightLoader     // 配置加载器（从ConfigMap读取）
	//metrics      *MetricsClient
	promClient promv1.API      // Prometheus查询客户端
	cache      *NodeUsageCache // 节点资源使用率缓存
}

// Name 必须实现framework.Plugin接口
// 作用：返回插件名称，用于日志和监控
func (d *DynamicWeight) Name() string {
	return Name
}

// 实现所有必需接口方法
var _ framework.ScorePlugin = &DynamicWeight{} // 实现评分插件接口
var _ framework.Plugin = &DynamicWeight{}      // 实现基础插件接口

// ScoreExtensions 实现Score扩展接口
func (d *DynamicWeight) ScoreExtensions() framework.ScoreExtensions {
	return nil
}

// Score 核心评分逻辑：计算节点得分
// 输入：
//   - ctx: 上下文（用于超时控制）
//   - state: 调度周期状态（暂未使用）
//   - pod: 待调度的Pod对象
//   - nodeName: 候选节点名称
//
// 输出：
//   - 节点得分（0-100分）
//   - 错误状态（成功时为nil）
func (d *DynamicWeight) Score(ctx context.Context,
	state *framework.CycleState,
	pod *v1.Pod,
	nodeName string) (int64, *framework.Status) {

	// 1. 获取权重配置
	args := d.weightLoader.GetWeights()

	// 2. 获取节点实时指标
	usage, err := d.getRealNodeUsage(nodeName)
	if err != nil {
		return 0, framework.AsStatus(fmt.Errorf("获取节点指标失败: %v", err))
	}

	// 3. 解析Pod标签,确定资源权重
	//labels := strings.Split(pod.Labels["resource-prefer"], "_")
	//	weights := args.DefaultWeights // 默认权重
	//	for _, label := range labels {
	//		if w, ok := args.LabelWeights[label]; ok {
	//			weights = w // 使用标签匹配的权重
	//			break
	//		}
	//	}
	labelValue, exists := pod.Labels["resource-prefer"]
	weights := args.DefaultWeights
	if exists {
		if w, ok := args.LabelWeights[labelValue]; ok {
			weights = w
		}
	}

	// 4. 计算加权得分
	//score := calculateScore(weights, usage)
	score := 0.0
	for res, weight := range weights {
		// 计算各资源维度贡献分：权重 × (1 - 使用率)
		//score += weight * (1 - usage.Get(res))
		switch res {
		case "cpu":
			score += weight * (1 - usage.CPU)
		case "memory":
			score += weight * (1 - usage.Memory)
		case "diskio":
			score += weight * (1 - usage.DiskIO)
		case "netio":
			score += weight * (1 - usage.Network)
		}
	}

	// 5. 记录日志
	klog.V(4).InfoS("节点评分结果",
		"pod", pod.Name,
		"node", nodeName,
		"score", score,
		"cpuUsage", usage.CPU,
		"memUsage", usage.Memory,
		"diskioUsage", usage.DiskIO, //新增
		"netioUsage", usage.Network, //新增
	)

	// 步骤5：转换为0-100分制
	return int64(score * 100), nil
}

// 初始化Prometheus客户端
func initPrometheusClient() (promv1.API, error) {
	client, err := prometheus.NewClient(prometheus.Config{
		Address: "http://prometheus-operated.monitoring.svc:9090",
	})
	if err != nil {
		return nil, err
	}
	return promv1.NewAPI(client), nil
}

// NodeUsage 节点资源使用率数据结构,在cache中已定义
//type NodeUsage struct {
//	CPU     float64
//	Memory  float64
//	DiskIO  float64
//	Network float64
//}

// 获取节点真实资源使用率
func (d *DynamicWeight) getRealNodeUsage(nodeName string) (*NodeUsage, error) {
	// 尝试从缓存获取
	if cached := d.cache.Get(nodeName); cached != nil {
		return cached, nil
	}
	// 1. 获取节点对象
	node, err := d.handle.ClientSet().CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("获取节点信息失败: %v", err)
	}

	// 2. 提取节点的内部IP
	var nodeIP string
	for _, addr := range node.Status.Addresses {
		if addr.Type == v1.NodeInternalIP {
			nodeIP = addr.Address
			break
		}
	}
	if nodeIP == "" {
		return nil, fmt.Errorf("节点 %s 无内部IP地址", nodeName)
	}

	// 定义Prometheus查询模板
	const (
		cpuQueryTemplate = `sum(rate(node_cpu_seconds_total{mode!="idle", instance=~"%s(:.*)?"}[5m])) 
                            / count(node_cpu_seconds_total{mode="user", instance=~"%s(:.*)?"})`

		memQueryTemplate = `(node_memory_MemTotal_bytes{instance=~"%s(:.*)?"} 
                            - node_memory_MemAvailable_bytes{instance=~"%s(:.*)?"}) 
                            / node_memory_MemTotal_bytes{instance=~"%s(:.*)?"}`

		diskQueryTemplate = `rate(node_disk_io_time_seconds_total{device=~"sdb", instance=~"%s(:.*)?"}[5m])`

		netQueryTemplate = `
                            (rate(node_network_receive_bytes_total{device="eth0", instance=~"%s(:.*)?"}[5m]) * 8 
                            + rate(node_network_transmit_bytes_total{device="eth0", instance=~"%s(:.*)?"}[5m]) * 8
                            ) / (node_network_speed_bytes{device="eth0", instance=~"%s(:.*)?"}) * 100` // 转换为百分比
	)

	// 执行CPU查询
	cpuQuery := fmt.Sprintf(cpuQueryTemplate, nodeIP, nodeIP)
	cpuValue, err := d.queryPrometheus(cpuQuery)
	if err != nil {
		return nil, fmt.Errorf("CPU查询失败: %v", err)
	}

	// 执行内存查询
	memQuery := fmt.Sprintf(memQueryTemplate, nodeIP, nodeIP, nodeIP)
	memValue, err := d.queryPrometheus(memQuery)
	if err != nil {
		return nil, fmt.Errorf("内存查询失败: %v", err)
	}

	// 执行磁盘IO查询
	diskQuery := fmt.Sprintf(diskQueryTemplate, nodeIP)
	diskValue, err := d.queryPrometheus(diskQuery)
	if err != nil {
		klog.Warningf("磁盘指标不可用，使用默认值: %v", err)
		diskValue = 0.3 // 降级处理
	}

	// 执行网络查询
	netQuery := fmt.Sprintf(netQueryTemplate, nodeIP, nodeIP, nodeIP)
	netValue, err := d.queryPrometheus(netQuery)
	if err != nil {
		klog.Warningf("网络指标不可用，使用默认值: %v", err)
		netValue = 0.2 // 降级处理
	}

	// 构建返回数据
	usage := &NodeUsage{
		CPU:     cpuValue,
		Memory:  memValue,
		DiskIO:  diskValue,
		Network: netValue,
	}

	// 更新缓存
	d.cache.Set(nodeName, usage)
	return usage, nil
}

// 统一Prometheus查询方法
func (d *DynamicWeight) queryPrometheus(query string) (float64, error) {
	result, _, err := d. (context.Background(), query, time.Now())
	if err != nil {
		return 0, err
	}

	// 解析向量类型结果
	vector, ok := result.(model.Vector)
	if !ok || len(vector) == 0 {
		return 0, fmt.Errorf("无效的查询结果格式")
	}

	return float64(vector[0].Value), nil
}

// 初始化工厂函数
func New(ctx context.Context, args runtime.Object, h framework.Handle) (framework.Plugin, error) {
	// 初始化Prometheus客户端
	promClient, err := initPrometheusClient()
	if err != nil {
		return nil, fmt.Errorf("初始化Prometheus客户端失败: %v", err)
	}

	// 初始化权重加载器
	weightLoader, err := NewWeightLoader(h.ClientSet())
	if err != nil {
		return nil, fmt.Errorf("配置加载失败: %v", err)
	}

	// 返回插件实例
	return &DynamicWeight{
		handle:       h,
		weightLoader: weightLoader,
		promClient:   promClient,
		cache:        NewNodeUsageCache(5 * time.Minute),
	}, nil
}
