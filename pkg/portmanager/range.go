package portmanager

import (
	"context"
	"fmt"
	"sync"

	"github.com/go-logr/logr"
	"github.com/tiggoins/nodeport-allocator/pkg/config"
	"github.com/tiggoins/nodeport-allocator/pkg/utils"
)

// PortRange 端口范围管理器
type PortRange struct {
	name    string
	config  config.PortRange
	bitSet  *utils.BitSet
	storage *Storage
	logger  logr.Logger
	mutex   sync.RWMutex
}

// NewPortRange 创建新的端口范围管理器
func NewPortRange(name string, config config.PortRange, storage *Storage, logger logr.Logger) *PortRange {
	return &PortRange{
		name:    name,
		config:  config,
		storage: storage,
		logger:  logger.WithValues("range", name),
	}
}

// Initialize 初始化端口范围
func (pr *PortRange) Initialize(ctx context.Context) error {
	pr.mutex.Lock()
	defer pr.mutex.Unlock()

	var err error
	pr.bitSet, err = pr.storage.LoadBitSet(ctx, pr.name, pr.config.Start, pr.config.End)
	if err != nil {
		return fmt.Errorf("初始化端口范围 %s 失败: %v", pr.name, err)
	}

	pr.logger.Info("端口范围初始化完成",
		"start", pr.config.Start,
		"end", pr.config.End,
		"used", pr.bitSet.Count(),
		"total", pr.config.End-pr.config.Start+1)

	return nil
}

// AllocatePort 分配端口
func (pr *PortRange) AllocatePort(ctx context.Context, requestedPort int32) (int32, error) {
	pr.mutex.Lock()
	defer pr.mutex.Unlock()

	if pr.bitSet == nil {
		return 0, fmt.Errorf("端口范围未初始化")
	}

	var port int32
	var err error

	if requestedPort != 0 {
		// 分配指定端口
		// 业务逻辑层检查：确保用户请求的端口在允许的范围内
		if requestedPort < pr.config.Start || requestedPort > pr.config.End {
			return 0, fmt.Errorf("端口 %d 超出允许的范围 [%d, %d]", requestedPort, pr.config.Start, pr.config.End)
		}

		if pr.bitSet.Test(requestedPort) {
			return 0, fmt.Errorf("端口 %d 已被使用", requestedPort)
		}

		port = requestedPort
	} else {
		// 自动分配端口
		var found bool
		port, found = pr.bitSet.FindFirstClear()
		if !found {
			return 0, fmt.Errorf("端口范围 %s 已满", pr.name)
		}
	}

	// 标记端口为已使用
	if err = pr.bitSet.Set(port); err != nil {
		return 0, fmt.Errorf("标记端口失败: %v", err)
	}

	// 保存到存储
	if err = pr.storage.SaveBitSet(ctx, pr.name, pr.bitSet); err != nil {
		// 回滚
		pr.bitSet.Clear(port)
		return 0, fmt.Errorf("保存端口状态失败: %v", err)
	}

	pr.logger.Info("端口分配成功", "port", port)
	return port, nil
}

// ReleasePort 释放端口
func (pr *PortRange) ReleasePort(ctx context.Context, port int32) error {
	pr.mutex.Lock()
	defer pr.mutex.Unlock()

	if pr.bitSet == nil {
		return fmt.Errorf("端口范围未初始化")
	}

	// 业务逻辑层检查：确保释放的端口在允许的范围内
	if port < pr.config.Start || port > pr.config.End {
		return fmt.Errorf("端口 %d 超出允许的范围 [%d, %d]", port, pr.config.Start, pr.config.End)
	}

	if !pr.bitSet.Test(port) {
		pr.logger.Info("端口未被使用，跳过释放", "port", port)
		return nil
	}

	// 清除端口标记
	if err := pr.bitSet.Clear(port); err != nil {
		return fmt.Errorf("清除端口标记失败: %v", err)
	}

	// 保存到存储
	if err := pr.storage.SaveBitSet(ctx, pr.name, pr.bitSet); err != nil {
		// 回滚
		pr.bitSet.Set(port)
		return fmt.Errorf("保存端口状态失败: %v", err)
	}

	pr.logger.Info("端口释放成功", "port", port)
	return nil
}

// MarkPortAsUsed 标记端口为已使用（用于初始化现有服务）
func (pr *PortRange) MarkPortAsUsed(ctx context.Context, port int32) error {
	pr.mutex.Lock()
	defer pr.mutex.Unlock()

	if pr.bitSet == nil {
		return fmt.Errorf("端口范围未初始化")
	}

	// 业务逻辑层检查：确保标记的端口在允许的范围内
	if port < pr.config.Start || port > pr.config.End {
		return fmt.Errorf("端口 %d 超出允许的范围 [%d, %d]", port, pr.config.Start, pr.config.End)
	}

	// 如果端口已经被标记为使用，直接返回成功
	if pr.bitSet.Test(port) {
		pr.logger.Info("端口已被标记为使用", "port", port)
		return nil
	}

	// 标记端口为已使用
	if err := pr.bitSet.Set(port); err != nil {
		return fmt.Errorf("标记端口失败: %v", err)
	}

	// 保存到存储
	if err := pr.storage.SaveBitSet(ctx, pr.name, pr.bitSet); err != nil {
		// 回滚
		pr.bitSet.Clear(port)
		return fmt.Errorf("保存端口状态失败: %v", err)
	}

	pr.logger.Info("端口标记为已使用", "port", port)
	return nil
}

// IsPortUsed 检查端口是否被使用
func (pr *PortRange) IsPortUsed(port int32) bool {
	pr.mutex.RLock()
	defer pr.mutex.RUnlock()

	if pr.bitSet == nil {
		return false
	}

	return pr.bitSet.Test(port)
}


// GetStats 获取端口使用统计
func (pr *PortRange) GetStats() PortRangeStats {
	pr.mutex.RLock()
	defer pr.mutex.RUnlock()

	stats := PortRangeStats{
		Name:        pr.name,
		Start:       pr.config.Start,
		End:         pr.config.End,
		Total:       pr.config.End - pr.config.Start + 1,
		Description: pr.config.Description,
	}

	if pr.bitSet != nil {
		stats.Used = int32(pr.bitSet.Count())
		stats.Available = stats.Total - stats.Used
		stats.UsageRate = float64(stats.Used) / float64(stats.Total) * 100
	}

	return stats
}

// PortRangeStats 端口范围统计信息
type PortRangeStats struct {
	Name        string  `json:"name"`
	Start       int32   `json:"start"`
	End         int32   `json:"end"`
	Total       int32   `json:"total"`
	Used        int32   `json:"used"`
	Available   int32   `json:"available"`
	UsageRate   float64 `json:"usage_rate"`
	Description string  `json:"description"`
}
