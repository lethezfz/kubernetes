// pkg/scheduler/framework/plugins/dynamicweight/config.go
package dynamicweight

import (
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

var scheme = runtime.NewScheme()
var codecs = serializer.NewCodecFactory(scheme)

// 类型注册：必须将自定义配置类型注册到 Scheme 中才能被序列化/反序列化
// 接口完整性：实现 runtime.Object 接口是 Kubernetes 资源对象的强制要求
// 深拷贝安全：对 map 等引用类型必须实现深度拷贝，避免配置污染
func init() {
	// 注册 Kubernetes 核心 API 资源（必须）
	corev1.AddToScheme(scheme)

	// 注册 DynamicWeightArgs 到 Scheme
	scheme.AddKnownTypes(
		schema.GroupVersion{Group: "kubescheduler.config.k8s.io", Version: "v1"},
		&DynamicWeightArgs{},
	)
}

// 定义插件的配置参数结构体
// 作用：存储从ConfigMap读取的权重配置
type DynamicWeightArgs struct {
	// 默认权重：当Pod没有指定资源偏好标签时使用
	// 键值对格式：资源类型 -> 权重值
	// 示例：{"cpu":0.25, "memory":0.25, "diskio":0.25, "netio":0.25}
	DefaultWeights map[string]float64 `json:"defaultWeights"`

	// 标签权重：根据Pod的标签匹配对应的权重配置
	// 键值对格式：标签名称 -> 资源权重配置
	// 示例："cpu-prefer"标签对应{"cpu":0.7, "memory":0.1, ...}
	LabelWeights map[string]map[string]float64 `json:"labelWeights"`
}

// Name 必须实现PluginFactory接口
func (d *DynamicWeightArgs) Name() string {
	return "DynamicWeight"
}

// 实现 runtime.Object 接口
// 添加 DeepCopyObject 方法实现
func (in *DynamicWeightArgs) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// 实现 GetObjectKind 方法（必需接口）
func (in *DynamicWeightArgs) GetObjectKind() schema.ObjectKind {
	return schema.EmptyObjectKind // 非版本化对象返回空
}

// 添加 DeepCopy 方法
//func (in *DynamicWeightArgs) DeepCopy() *DynamicWeightArgs {
//	if in == nil {
//		return nil
//	}
//	out := new(DynamicWeightArgs)
//
//	// 深度复制 map 字段
//	out.DefaultWeights = make(map[string]float64)
//	for k, v := range in.DefaultWeights {
//		out.DefaultWeights[k] = v
//	}
//
//	out.LabelWeights = make(map[string]map[string]float64)
//	for label, weights := range in.LabelWeights {
//		newWeights := make(map[string]float64)
//		for k, v := range weights {
//			newWeights[k] = v
//		}
//		out.LabelWeights[label] = newWeights
//	}
//
//	return out
//}

// NewDynamicWeightArgs 创建配置实例
// 创建配置实例的工厂函数
// 输入：Kubernetes API对象（包含从ConfigMap读取的原始数据）
// 输出：初始化后的DynamicWeightArgs指针和错误信息
func NewDynamicWeightArgs(obj runtime.Object) (*DynamicWeightArgs, error) {
	// 步骤1：设置默认配置
	args := &DynamicWeightArgs{
		DefaultWeights: map[string]float64{
			"cpu":    0.25,
			"memory": 0.25,
			"diskio": 0.25,
			"netio":  0.25,
		},
		LabelWeights: make(map[string]map[string]float64),
	}

	// 步骤2：如果有输入配置，则解析覆盖默认值
	if obj != nil {
		// 将Kubernetes的原始数据转换为结构体
		// 类似JSON反序列化过程
		raw := obj.(*runtime.Unknown).Raw
		decoded, _, err := codecs.UniversalDeserializer().Decode(raw, nil, nil)
		if err != nil {
			return nil, fmt.Errorf("解码失败: %v", err)
		}

		var ok bool
		args, ok = decoded.(*DynamicWeightArgs)
		if !ok {
			return nil, fmt.Errorf("无效的配置类型: %T", decoded)
		}
	}

	return args, nil
}
