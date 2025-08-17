package portmanager

import (
	"context"
	"fmt"
	"sync"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/tiggoins/nodeport-allocator/pkg/config"
)

// Manager 端口管理器
type Manager struct {
	ctx       context.Context
	client    client.Client
	config    *config.Config
	storage   *Storage
	ranges    map[string]*PortRange
	allocator *Allocator
	logger    logr.Logger
	mutex     sync.RWMutex
}

// NewManager 创建新的端口管理器
func NewManager(ctx context.Context, client client.Client, config *config.Config, logger logr.Logger) (*Manager, error) {
	storage, err := NewStorage(client, &config.StorageConfig, logger.WithName("storage"))
	if err != nil {
		return nil, fmt.Errorf("创建存储失败: %v", err)
	}

	manager := &Manager{
		ctx:     ctx,
		client:  client,
		config:  config,
		storage: storage,
		ranges:  make(map[string]*PortRange),
		logger:  logger,
	}

	manager.allocator = NewAllocator(manager, logger.WithName("allocator"))

	return manager, nil
}

// Initialize 初始化端口管理器
func (m *Manager) Initialize(ctx context.Context) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	for name, rangeConfig := range m.config.PortRanges {
		portRange := NewPortRange(name, rangeConfig, m.storage, m.logger)
		if err := portRange.Initialize(ctx); err != nil {
			return fmt.Errorf("初始化端口范围 %s 失败: %v", name, err)
		}
		m.ranges[name] = portRange
	}

	m.logger.Info("端口管理器初始化完成", "ranges", len(m.ranges))
	return nil
}

// GetPortRange 获取端口范围管理器
func (m *Manager) GetPortRange(name string) *PortRange {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.ranges[name]
}

// GetAllocator 获取端口分配器
func (m *Manager) GetAllocator() *Allocator {
	return m.allocator
}

// GetConfig 获取配置
func (m *Manager) GetConfig() *config.Config {
	return m.config
}

// ScanExistingServices 扫描现有的NodePort Services并初始化端口状态
func (m *Manager) ScanExistingServices(ctx context.Context) error {
	m.logger.Info("开始扫描现有NodePort Services")

	// 列出所有Services
	var serviceList corev1.ServiceList
	if err := m.client.List(ctx, &serviceList); err != nil {
		return fmt.Errorf("列出Services失败: %v", err)
	}

	m.logger.Info("找到Services", "count", len(serviceList.Items))

	// 处理每个NodePort Service
	for _, service := range serviceList.Items {
		// 只处理NodePort类型的Service
		if service.Spec.Type != corev1.ServiceTypeNodePort {
			continue
		}

		namespace := service.Namespace
		if namespace == "" {
			namespace = "default"
		}

		m.logger.Info("处理NodePort Service", "namespace", namespace, "name", service.Name)

		// 获取对应的端口范围
		rangeName, portRange, err := m.config.GetPortRangeForService(namespace, service.Labels)
		if err != nil {
			m.logger.Error(err, "获取Service端口范围失败", "namespace", namespace, "name", service.Name)
			continue
		}

		rangeManager := m.GetPortRange(rangeName)
		if rangeManager == nil {
			m.logger.Error(fmt.Errorf("端口范围管理器不存在"), "端口范围管理器不存在", "range", rangeName)
			continue
		}

		// 标记已使用的端口
		for _, port := range service.Spec.Ports {
			// 业务逻辑层检查：验证现有Service的端口是否在配置范围内
			// 这在启动时扫描现有服务时使用，以确保端口状态与实际集群状态一致
			if port.NodePort < portRange.Start || port.NodePort > portRange.End {
				if !m.config.AllowOutsideRangePorts {
					m.logger.Info("Service使用的NodePort不在配置范围内，跳过标记",
						"namespace", namespace,
						"name", service.Name,
						"port", port.NodePort,
						"range", rangeName,
						"rangeStart", portRange.Start,
						"rangeEnd", portRange.End)
					continue
				} else {
					m.logger.Info("Service使用的NodePort不在配置范围内，但允许外部端口",
						"namespace", namespace,
						"name", service.Name,
						"port", port.NodePort,
						"range", rangeName,
						"rangeStart", portRange.Start,
						"rangeEnd", portRange.End)
					// 继续处理，标记端口为已使用
				}
			}

			// 标记端口为已使用
			if err := rangeManager.MarkPortAsUsed(ctx, port.NodePort); err != nil {
				m.logger.Error(err, "标记端口为已使用失败",
					"namespace", namespace,
					"name", service.Name,
					"port", port.NodePort)
			} else {
				m.logger.Info("成功标记端口为已使用",
					"namespace", namespace,
					"name", service.Name,
					"port", port.NodePort)
			}
		}
	}

	m.logger.Info("完成扫描现有NodePort Services")
	return nil
}

// ValidatePortForService 验证Service的端口是否合法（支持标签）
func (m *Manager) ValidatePortForService(namespace string, labels map[string]string, port int32) error {
	_, portRange, err := m.config.GetPortRangeForService(namespace, labels)
	if err != nil {
		return err
	}

	if port < portRange.Start || port > portRange.End {
		// 检查是否允许超出范围的端口
		if !m.config.AllowOutsideRangePorts {
			return fmt.Errorf("端口 %d 超出允许的范围 [%d, %d]", port, portRange.Start, portRange.End)
		}
	}

	return nil
}
