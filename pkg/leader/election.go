package leader

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

// Election Leader选举管理器
type Election struct {
	client    kubernetes.Interface
	lockName  string
	onStarted func(context.Context)
	onStopped func()
	logger    logr.Logger
	cancel    context.CancelFunc
}

// NewElection 创建Leader选举管理器
func NewElection(client kubernetes.Interface, lockName string, onStarted func(context.Context), logger logr.Logger) *Election {
	return &Election{
		client:    client,
		lockName:  lockName,
		onStarted: onStarted,
		logger:    logger,
	}
}

// Start 启动Leader选举
func (e *Election) Start(ctx context.Context) error {
	e.logger.Info("启动Leader选举", "lockName", e.lockName)

	// 获取当前Pod信息
	podName := getEnvOrDefault("POD_NAME", "unknown")
	namespace := getEnvOrDefault("POD_NAMESPACE", "default")

	// 创建资源锁
	lock, err := resourcelock.New(
		resourcelock.LeasesResourceLock,
		namespace,
		e.lockName,
		e.client.CoreV1(),
		nil,
		resourcelock.ResourceLockConfig{
			Identity: podName,
		},
	)
	if err != nil {
		return fmt.Errorf("创建资源锁失败: %v", err)
	}

	// 创建Leader选举配置
	config := leaderelection.LeaderElectionConfig{
		Lock:            lock,
		ReleaseOnCancel: true,
		LeaseDuration:   15 * time.Second,
		RenewDeadline:   10 * time.Second,
		RetryPeriod:     2 * time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				e.logger.Info("成为Leader")
				if e.onStarted != nil {
					e.onStarted(ctx)
				}
			},
			OnStoppedLeading: func() {
				e.logger.Info("失去Leader地位")
				if e.onStopped != nil {
					e.onStopped()
				}
			},
			OnNewLeader: func(identity string) {
				if identity == podName {
					return
				}
				e.logger.Info("新的Leader", "leader", identity)
			},
		},
	}

	// 创建可取消的上下文
	ctx, cancel := context.WithCancel(ctx)
	e.cancel = cancel

	// 启动Leader选举
	go func() {
		leaderelection.RunOrDie(ctx, config)
	}()

	// 等待上下文取消
	<-ctx.Done()
	e.logger.Info("Leader选举已停止")
	return nil
}

// Stop 停止Leader选举
func (e *Election) Stop() error {
	e.logger.Info("停止Leader选举")
	if e.cancel != nil {
		e.cancel()
	}
	return nil
}

// NeedLeaderElection 实现manager.LeaderElectionRunnable接口
func (e *Election) NeedLeaderElection() bool {
	return true
}

// getEnvOrDefault 获取环境变量或默认值
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
