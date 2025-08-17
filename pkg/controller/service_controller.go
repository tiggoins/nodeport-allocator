package controller

import (
	"context"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/tiggoins/nodeport-allocator/pkg/portmanager"
)

// ServiceReconciler Service资源协调器
type ServiceReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	PortManager *portmanager.Manager
	Logger      logr.Logger
}

// Reconcile 协调Service资源
func (r *ServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger.WithValues("service", req.NamespacedName)
	logger.Info("开始协调Service")

	// 获取Service对象
	var service corev1.Service
	if err := r.Get(ctx, req.NamespacedName, &service); err != nil {
		if client.IgnoreNotFound(err) == nil {
			// Service已被删除，执行清理
			logger.Info("Service已删除，执行端口回收")
			return r.handleServiceDeletion(ctx, req.NamespacedName)
		}
		logger.Error(err, "获取Service失败")
		return ctrl.Result{}, err
	}

	// 检查是否正在删除
	if !service.DeletionTimestamp.IsZero() {
		logger.Info("Service正在删除，执行端口回收")
		return r.handleServiceDeletion(ctx, req.NamespacedName)
	}

	// 只处理NodePort类型的Service
	if service.Spec.Type != corev1.ServiceTypeNodePort {
		logger.Info("跳过非NodePort类型的Service", "type", service.Spec.Type)
		return ctrl.Result{}, nil
	}

	// 添加Finalizer以确保端口回收
	if !controllerutil.ContainsFinalizer(&service, "nodeport-allocator.example.com/finalizer") {
		controllerutil.AddFinalizer(&service, "nodeport-allocator.example.com/finalizer")
		if err := r.Update(ctx, &service); err != nil {
			logger.Error(err, "添加Finalizer失败")
			return ctrl.Result{}, err
		}
		logger.Info("已添加Finalizer")
	}

	logger.Info("Service协调完成")
	return ctrl.Result{}, nil
}

// handleServiceDeletion 处理Service删除
func (r *ServiceReconciler) handleServiceDeletion(ctx context.Context, namespacedName client.ObjectKey) (ctrl.Result, error) {
	logger := r.Logger.WithValues("service", namespacedName)

	// 尝试获取Service以进行端口回收
	var service corev1.Service
	if err := r.Get(ctx, namespacedName, &service); err != nil {
		if client.IgnoreNotFound(err) == nil {
			logger.Info("Service不存在，跳过端口回收")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "获取Service失败")
		return ctrl.Result{}, err
	}

	// 执行端口回收
	if service.Spec.Type == corev1.ServiceTypeNodePort {
		allocator := r.PortManager.GetAllocator()
		if err := allocator.ReleaseForService(ctx, &service); err != nil {
			logger.Error(err, "端口回收失败")
			// 不阻塞删除过程，只记录错误
		}
	}

	// 移除Finalizer
	if controllerutil.ContainsFinalizer(&service, "nodeport-allocator.example.com/finalizer") {
		controllerutil.RemoveFinalizer(&service, "nodeport-allocator.example.com/finalizer")
		if err := r.Update(ctx, &service); err != nil {
			logger.Error(err, "移除Finalizer失败")
			return ctrl.Result{}, err
		}
		logger.Info("已移除Finalizer")
	}

	logger.Info("Service删除处理完成")
	return ctrl.Result{}, nil
}

// SetupWithManager 设置控制器
func (r *ServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Service{}).
		Complete(r)
}
