package utils

import (
    "github.com/go-logr/logr"
    ctrl "sigs.k8s.io/controller-runtime"
)

// NewLogger 创建新的日志记录器
func NewLogger(name string) logr.Logger {
    return ctrl.Log.WithName(name)
}
