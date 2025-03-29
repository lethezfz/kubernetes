/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package plugins

import (
	"k8s.io/apiserver/pkg/util/feature"
	"k8s.io/kubernetes/pkg/features"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/defaultbinder"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/defaultpreemption"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/dynamicresources"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/dynamicweight" //注册dynamicweight
	plfeature "k8s.io/kubernetes/pkg/scheduler/framework/plugins/feature"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/imagelocality"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/interpodaffinity"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/nodeaffinity"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/nodename"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/nodeports"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/noderesources"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/nodeunschedulable"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/nodevolumelimits"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/podtopologyspread"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/queuesort"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/schedulinggates"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/tainttoleration"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/volumebinding"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/volumerestrictions"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/volumezone"
	"k8s.io/kubernetes/pkg/scheduler/framework/runtime"
)

// NewInTreeRegistry builds the registry with all the in-tree plugins.
// A scheduler that runs out of tree plugins can register additional plugins
// through the WithFrameworkOutOfTreeRegistry option.
func NewInTreeRegistry() runtime.Registry {
	fts := plfeature.Features{
		EnableDRAAdminAccess:                         feature.DefaultFeatureGate.Enabled(features.DRAAdminAccess),
		EnableDynamicResourceAllocation:              feature.DefaultFeatureGate.Enabled(features.DynamicResourceAllocation),
		EnableVolumeCapacityPriority:                 feature.DefaultFeatureGate.Enabled(features.VolumeCapacityPriority),
		EnableNodeInclusionPolicyInPodTopologySpread: feature.DefaultFeatureGate.Enabled(features.NodeInclusionPolicyInPodTopologySpread),
		EnableMatchLabelKeysInPodTopologySpread:      feature.DefaultFeatureGate.Enabled(features.MatchLabelKeysInPodTopologySpread),
		EnableInPlacePodVerticalScaling:              feature.DefaultFeatureGate.Enabled(features.InPlacePodVerticalScaling),
		EnableSidecarContainers:                      feature.DefaultFeatureGate.Enabled(features.SidecarContainers),
		EnableSchedulingQueueHint:                    feature.DefaultFeatureGate.Enabled(features.SchedulerQueueingHints),
		EnableAsyncPreemption:                        feature.DefaultFeatureGate.Enabled(features.SchedulerAsyncPreemption),
		EnablePodLevelResources:                      feature.DefaultFeatureGate.Enabled(features.PodLevelResources),
		//在插件注册代码中误引用了不存在的特性开关（Feature Gate），而自定义调度插件通常不需要特性门控。
		//EnableDynamicWeight:                          feature.DefaultFeatureGate.Enabled(features.DynamicWeight), //注册dynamicweight
	}

	registry := runtime.Registry{
		dynamicresources.Name:                runtime.FactoryAdapter(fts, dynamicresources.New),
		imagelocality.Name:                   imagelocality.New,
		tainttoleration.Name:                 runtime.FactoryAdapter(fts, tainttoleration.New),
		nodename.Name:                        runtime.FactoryAdapter(fts, nodename.New),
		nodeports.Name:                       runtime.FactoryAdapter(fts, nodeports.New),
		nodeaffinity.Name:                    runtime.FactoryAdapter(fts, nodeaffinity.New),
		podtopologyspread.Name:               runtime.FactoryAdapter(fts, podtopologyspread.New),
		nodeunschedulable.Name:               runtime.FactoryAdapter(fts, nodeunschedulable.New),
		noderesources.Name:                   runtime.FactoryAdapter(fts, noderesources.NewFit),
		noderesources.BalancedAllocationName: runtime.FactoryAdapter(fts, noderesources.NewBalancedAllocation),
		volumebinding.Name:                   runtime.FactoryAdapter(fts, volumebinding.New),
		volumerestrictions.Name:              runtime.FactoryAdapter(fts, volumerestrictions.New),
		volumezone.Name:                      runtime.FactoryAdapter(fts, volumezone.New),
		nodevolumelimits.CSIName:             runtime.FactoryAdapter(fts, nodevolumelimits.NewCSI),
		interpodaffinity.Name:                runtime.FactoryAdapter(fts, interpodaffinity.New),
		queuesort.Name:                       queuesort.New,
		defaultbinder.Name:                   defaultbinder.New,
		defaultpreemption.Name:               runtime.FactoryAdapter(fts, defaultpreemption.New),
		schedulinggates.Name:                 runtime.FactoryAdapter(fts, schedulinggates.New),
		dynamicweight.Name:                   runtime.PluginFactory(dynamicweight.New),
		//dynamicweight.Name:                 runtime.FactoryAdapter(fts, dynamicweight.New), //注册dynamicweight
		//Kubernetes 要求插件工厂函数必须符合 PluginFactoryWithFts 类型（接收 framework.Framework 参数），但当前实现的 dynamicweight.New 函数使用的是 framework.Handle 参数，导致签名不匹配。
	}

	return registry
}
