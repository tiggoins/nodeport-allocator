package utils

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RetryOnConflict 使用指数退避重试机制，处理更新冲突
func RetryOnConflict(ctx context.Context, attempts int, delay time.Duration, fn func() error) error {
	backoff := wait.Backoff{
		Steps:    attempts,
		Duration: delay,
		Factor:   1.5, // 每次递增
		Jitter:   0.1, // 随机抖动
	}

	return wait.ExponentialBackoffWithContext(ctx, backoff, func(ctx context.Context) (bool, error) {
		if err := fn(); err != nil {
			if apierrors.IsConflict(err) {
				return false, nil // 重试
			}
			return false, err // 其他错误直接返回
		}
		return true, nil
	})
}

// GetObject 获取 Kubernetes 对象，带重试机制
func GetObject(ctx context.Context, c client.Client, key client.ObjectKey, obj client.Object, attempts int, delay time.Duration) error {
	return RetryOnConflict(ctx, attempts, delay, func() error {
		return c.Get(ctx, key, obj)
	})
}

// UpdateObject 更新 Kubernetes 对象，带重试机制
func UpdateObject(ctx context.Context, c client.Client, obj client.Object, attempts int, delay time.Duration) error {
	return RetryOnConflict(ctx, attempts, delay, func() error {
		return c.Update(ctx, obj)
	})
}

// CreateObject 创建 Kubernetes 对象，带重试机制
func CreateObject(ctx context.Context, c client.Client, obj client.Object, attempts int, delay time.Duration) error {
	return RetryOnConflict(ctx, attempts, delay, func() error {
		return c.Create(ctx, obj)
	})
}

// DeleteObject 删除 Kubernetes 对象，带重试机制
func DeleteObject(ctx context.Context, c client.Client, obj client.Object, attempts int, delay time.Duration) error {
	return RetryOnConflict(ctx, attempts, delay, func() error {
		return c.Delete(ctx, obj)
	})
}

// IsObjectNotFound 检查对象是否不存在
func IsObjectNotFound(err error) bool {
	return apierrors.IsNotFound(err)
}

// GenerateName 生成资源名称（带时间戳，保证唯一性）
func GenerateName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// GetNamespaceFromObject 从对象获取命名空间（默认返回 default）
func GetNamespaceFromObject(obj metav1.Object) string {
	ns := obj.GetNamespace()
	if ns == "" {
		return "default"
	}
	return ns
}
