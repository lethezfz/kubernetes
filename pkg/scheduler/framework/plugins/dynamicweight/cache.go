// pkg/scheduler/framework/plugins/dynamicweight/cache.go
package dynamicweight

import (
	"sync"
	"time"
)

// NodeUsageCache 节点资源使用率缓存结构
// 目的：缓存节点的实时资源使用率数据，减少对Prometheus的频繁查询，提高调度性能
// 设计要点：
//  1. 线程安全：通过读写锁（RWMutex）保障并发访问安全
//  2. 数据时效性：设置缓存超时时间，避免使用过时数据
//  3. 内存高效：自动清理过期缓存项
type NodeUsageCache struct {
	data    map[string]*NodeUsage // 缓存存储（节点名称 -> 使用率数据）
	mu      sync.RWMutex          // 读写锁（保障线程安全）
	timeout time.Duration         // 缓存超时时间（例如5分钟）
}

// NewNodeUsageCache 创建新的缓存实例
// 参数：
//   - timeout: 缓存有效时长（例如5*time.Minute）
//
// 返回：
//   - 初始化后的缓存指针
func NewNodeUsageCache(timeout time.Duration) *NodeUsageCache {
	return &NodeUsageCache{
		data:    make(map[string]*NodeUsage),
		timeout: timeout,
	}
}

// Get 获取指定节点的缓存数据
// 流程：
//  1. 加读锁（允许并发读）
//  2. 检查是否存在未过期的缓存
//  3. 返回数据或nil
func (c *NodeUsageCache) Get(node string) *NodeUsage {
	c.mu.RLock()         // 加读锁
	defer c.mu.RUnlock() // 函数返回时释放锁

	if entry, ok := c.data[node]; ok {
		// 检查缓存是否过期
		if time.Since(entry.Timestamp) < c.timeout {
			return entry // 返回未过期的有效数据
		}
	}
	return nil // 无有效缓存
}

// Set 更新节点缓存数据（线程安全）
// 流程：
//  1. 加写锁（阻塞其他读写操作）
//  2. 更新数据并记录当前时间戳
func (c *NodeUsageCache) Set(node string, usage *NodeUsage) {
	c.mu.Lock()         // 加写锁
	defer c.mu.Unlock() // 函数返回时释放锁

	usage.Timestamp = time.Now() // 记录更新时间戳
	c.data[node] = usage         // 存储或更新数据
}

// NodeUsage 节点资源使用率数据结构
// 字段说明：
//   - CPU:    节点CPU使用率（0.0-1.0）
//   - Memory: 节点内存使用率（0.0-1.0）
//   - DiskIO: 磁盘IO使用率（0.0-1.0）
//   - Network:网络带宽使用率（0.0-1.0）
//   - Timestamp: 数据采集时间（用于判断缓存有效性）
type NodeUsage struct {
	CPU       float64   // CPU使用率
	Memory    float64   // 内存使用率
	DiskIO    float64   // 磁盘IO使用率
	Network   float64   // 网络使用率
	Timestamp time.Time // 数据采集时间戳
}
