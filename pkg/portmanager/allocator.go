package portmanager

import (
    "context"
    "fmt"

    "github.com/go-logr/logr"
    corev1 "k8s.io/api/core/v1"
)

// Allocator 端口分配器
type Allocator struct {
    manager *Manager
    logger  logr.Logger
}

// NewAllocator 创建新的端口分配器
func NewAllocator(manager *Manager, logger logr.Logger) *Allocator {
    return &Allocator{
        manager: manager,
        logger:  logger,
    }
}

// AllocateForService 为Service分配端口
func (a *Allocator) AllocateForService(ctx context.Context, service *corev1.Service) ([]AllocationResult, error) {
    namespace := service.Namespace
    if namespace == "" {
        namespace = "default"
    }

    // 获取对应的端口范围（优先基于标签，回退到基于namespace）
    rangeName, portRange, err := a.manager.config.GetPortRangeForService(namespace, service.Labels)
    if err != nil {
        return nil, fmt.Errorf("获取Service %s/%s 的端口范围失败: %v", namespace, service.Name, err)
    }

    rangeManager := a.manager.GetPortRange(rangeName)
    if rangeManager == nil {
        return nil, fmt.Errorf("端口范围管理器 %s 不存在", rangeName)
    }

    var results []AllocationResult
    
    for i, port := range service.Spec.Ports {
        if port.NodePort == 0 {
            // 自动分配端口
            allocatedPort, err := rangeManager.AllocatePort(ctx, 0)
            if err != nil {
                // 回滚已分配的端口
                a.rollbackAllocations(ctx, results)
                return nil, fmt.Errorf("为端口 %s 分配 NodePort 失败: %v", port.Name, err)
            }
            
            results = append(results, AllocationResult{
                PortIndex:     i,
                PortName:      port.Name,
                AllocatedPort: allocatedPort,
                RangeName:     rangeName,
                Message:       fmt.Sprintf("自动分配 NodePort %d (范围: %s)", allocatedPort, rangeName),
            })
        } else {
            // 验证指定的端口
            if port.NodePort < portRange.Start || port.NodePort > portRange.End {
                // 检查是否允许超出范围的端口
                if !a.manager.config.AllowOutsideRangePorts {
                    return nil, fmt.Errorf("指定的 NodePort %d 超出命名空间 %s 允许的范围 [%d, %d]", 
                        port.NodePort, namespace, portRange.Start, portRange.End)
                } else {
                    a.logger.Info("允许使用超出范围的NodePort", 
                        "port", port.NodePort, 
                        "namespace", namespace, 
                        "rangeStart", portRange.Start, 
                        "rangeEnd", portRange.End)
                }
            }
            
            if rangeManager.IsPortUsed(port.NodePort) {
                return nil, fmt.Errorf("指定的 NodePort %d 已被使用", port.NodePort)
            }
            
            // 分配指定端口
            _, err := rangeManager.AllocatePort(ctx, port.NodePort)
            if err != nil {
                // 回滚已分配的端口
                a.rollbackAllocations(ctx, results)
                return nil, fmt.Errorf("分配指定 NodePort %d 失败: %v", port.NodePort, err)
            }
            
            results = append(results, AllocationResult{
                PortIndex:     i,
                PortName:      port.Name,
                AllocatedPort: port.NodePort,
                RangeName:     rangeName,
                Message:       fmt.Sprintf("使用指定 NodePort %d (范围: %s)", port.NodePort, rangeName),
            })
        }
    }

    a.logger.Info("端口分配完成",
        "service", fmt.Sprintf("%s/%s", namespace, service.Name),
        "range", rangeName,
        "allocated", len(results))

    return results, nil
}

// ReleaseForService 释放Service使用的端口
func (a *Allocator) ReleaseForService(ctx context.Context, service *corev1.Service) error {
    namespace := service.Namespace
    if namespace == "" {
        namespace = "default"
    }

    // 获取对应的端口范围（优先基于标签，回退到基于namespace）
    rangeName, _, err := a.manager.config.GetPortRangeForService(namespace, service.Labels)
    if err != nil {
        a.logger.Error(err, "获取Service端口范围失败", "namespace", namespace, "service", service.Name)
        return nil // 不阻塞删除流程
    }

    rangeManager := a.manager.GetPortRange(rangeName)
    if rangeManager == nil {
        a.logger.Error(fmt.Errorf("端口范围管理器不存在"), "端口范围管理器不存在", "range", rangeName)
        return nil // 不阻塞删除流程
    }

    var errors []error
    for _, port := range service.Spec.Ports {
        if port.NodePort != 0 {
            if err := rangeManager.ReleasePort(ctx, port.NodePort); err != nil {
                a.logger.Error(err, "释放端口失败", "port", port.NodePort, "service", fmt.Sprintf("%s/%s", namespace, service.Name))
                errors = append(errors, err)
            }
        }
    }

    if len(errors) > 0 {
        return fmt.Errorf("释放 %d 个端口时出现错误", len(errors))
    }

    a.logger.Info("端口释放完成",
        "service", fmt.Sprintf("%s/%s", namespace, service.Name),
        "range", rangeName)

    return nil
}

// rollbackAllocations 回滚端口分配
func (a *Allocator) rollbackAllocations(ctx context.Context, results []AllocationResult) {
    for _, result := range results {
        rangeManager := a.manager.GetPortRange(result.RangeName)
        if rangeManager != nil {
            if err := rangeManager.ReleasePort(ctx, result.AllocatedPort); err != nil {
                a.logger.Error(err, "回滚端口分配失败", "port", result.AllocatedPort)
            }
        }
    }
}

// AllocationResult 分配结果
type AllocationResult struct {
    PortIndex     int    `json:"port_index"`
    PortName      string `json:"port_name"`
    AllocatedPort int32  `json:"allocated_port"`
    RangeName     string `json:"range_name"`
    Message       string `json:"message"`
}
