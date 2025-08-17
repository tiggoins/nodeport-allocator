package portmanager

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/tiggoins/nodeport-allocator/pkg/config"
	"github.com/tiggoins/nodeport-allocator/pkg/utils"
)

// Storage ConfigMap存储实现
type Storage struct {
	client     client.Client
	config     *config.StorageConfig
	logger     logr.Logger
	retryDelay time.Duration
}

// NewStorage 创建新的存储实例
func NewStorage(client client.Client, config *config.StorageConfig, logger logr.Logger) (*Storage, error) {
	retryDelay, err := time.ParseDuration(config.RetryDelay)
	if err != nil {
		return nil, fmt.Errorf("解析重试延迟失败: %v", err)
	}

	return &Storage{
		client:     client,
		config:     config,
		logger:     logger,
		retryDelay: retryDelay,
	}, nil
}

// LoadBitSet 从ConfigMap加载位图数据
func (s *Storage) LoadBitSet(ctx context.Context, rangeName string, start, end int32) (*utils.BitSet, error) {
	cm, err := s.getConfigMap(ctx)
	if err != nil {
		if utils.IsObjectNotFound(err) {
			// 如果ConfigMap不存在，创建新的位图
			s.logger.Info("ConfigMap不存在，创建新的位图", "range", rangeName)
			return utils.NewBitSet(start, end), nil
		}
		return nil, fmt.Errorf("获取ConfigMap失败: %v", err)
	}

	data, exists := cm.Data[rangeName]
	if !exists {
		// 如果范围数据不存在，创建新的位图
		s.logger.Info("端口范围数据不存在，创建新位图", "range", rangeName)
		return utils.NewBitSet(start, end), nil
	}

	bitSet := utils.NewBitSet(start, end)
	if err := bitSet.FromJSON([]byte(data)); err != nil {
		s.logger.Error(err, "反序列化位图失败，创建新位图", "range", rangeName)
		return utils.NewBitSet(start, end), nil
	}

	s.logger.Info("成功加载位图数据", "range", rangeName, "used", bitSet.Count())
	return bitSet, nil
}

// SaveBitSet 保存位图数据到ConfigMap
func (s *Storage) SaveBitSet(ctx context.Context, rangeName string, bitSet *utils.BitSet) error {
	return utils.RetryOnConflict(ctx, s.config.RetryAttempts, s.retryDelay, func() error {
		return s.saveBitSetOnce(ctx, rangeName, bitSet)
	})
}

// saveBitSetOnce 单次保存位图数据
func (s *Storage) saveBitSetOnce(ctx context.Context, rangeName string, bitSet *utils.BitSet) error {
	data, err := bitSet.ToJSON()
	if err != nil {
		return fmt.Errorf("序列化位图失败: %v", err)
	}

	cm, err := s.getConfigMap(ctx)
	if err != nil {
		if utils.IsObjectNotFound(err) {
			// 创建新的ConfigMap
			s.logger.Info("创建新的ConfigMap", "name", s.config.ConfigMapName)
			return s.createConfigMap(ctx, rangeName, string(data))
		}
		return fmt.Errorf("获取ConfigMap失败: %v", err)
	}

	// 更新现有ConfigMap
	if cm.Data == nil {
		cm.Data = make(map[string]string)
	}
	cm.Data[rangeName] = string(data)

	if err := utils.UpdateObject(ctx, s.client, cm, 1, time.Second); err != nil {
		return fmt.Errorf("更新ConfigMap失败: %v", err)
	}

	s.logger.Info("位图数据保存成功", "range", rangeName, "used", bitSet.Count())
	return nil
}

// getConfigMap 获取ConfigMap
func (s *Storage) getConfigMap(ctx context.Context) (*corev1.ConfigMap, error) {
	cm := &corev1.ConfigMap{}
	key := types.NamespacedName{
		Name:      s.config.ConfigMapName,
		Namespace: s.config.ConfigMapNamespace,
	}

	err := s.client.Get(ctx, key, cm)
	return cm, err
}

// createConfigMap 创建新的ConfigMap
func (s *Storage) createConfigMap(ctx context.Context, rangeName, data string) error {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.config.ConfigMapName,
			Namespace: s.config.ConfigMapNamespace,
			Labels: map[string]string{
				"app":       "nodeport-allocator",
				"component": "storage",
			},
			Annotations: map[string]string{
				"nodeport-allocator.example.com/description": "NodePort端口使用状态存储",
				"nodeport-allocator.example.com/version":     "v1",
			},
		},
		Data: map[string]string{
			rangeName: data,
		},
	}

	if err := utils.CreateObject(ctx, s.client, cm, 1, time.Second); err != nil {
		return fmt.Errorf("创建ConfigMap失败: %v", err)
	}

	s.logger.Info("ConfigMap创建成功", "name", s.config.ConfigMapName, "namespace", s.config.ConfigMapNamespace)
	return nil
}








