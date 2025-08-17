package admission

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-logr/logr"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"

	"github.com/tiggoins/nodeport-allocator/pkg/portmanager"
)

// Mutator MutatingAdmissionWebhook实现
type Mutator struct {
	portManager *portmanager.Manager
	logger      logr.Logger
	decoder     runtime.Decoder
}

// NewMutator 创建新的变更器
func NewMutator(portManager *portmanager.Manager, logger logr.Logger) *Mutator {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = admissionv1.AddToScheme(scheme)

	return &Mutator{
		portManager: portManager,
		logger:      logger,
		decoder:     serializer.NewCodecFactory(scheme).UniversalDeserializer(),
	}
}

// Handle 处理准入请求
func (m *Mutator) Handle(ctx context.Context, req *admissionv1.AdmissionRequest) *admissionv1.AdmissionResponse {
	logger := m.logger.WithValues("uid", req.UID, "kind", req.Kind, "namespace", req.Namespace, "name", req.Name)
	logger.Info("处理准入请求")

	// 只处理Service资源
	if req.Kind.Kind != "Service" || req.Kind.Version != "v1" {
		logger.Info("跳过非Service资源")
		return NewAdmissionResponse(req.UID).Allow().AdmissionResponse
	}

	// 解析Service对象
	var service corev1.Service
	if err := runtime.DecodeInto(m.decoder, req.Object.Raw, &service); err != nil {
		logger.Error(err, "解析Service对象失败")
		return NewAdmissionResponse(req.UID).Deny(fmt.Sprintf("解析Service对象失败: %v", err)).AdmissionResponse
	}

	// 只处理NodePort类型的Service
	if service.Spec.Type != corev1.ServiceTypeNodePort {
		logger.Info("跳过非NodePort类型的Service", "type", service.Spec.Type)
		return NewAdmissionResponse(req.UID).Allow().AdmissionResponse
	}

	// 处理端口分配和验证
	mutation, err := m.processService(ctx, &service, req.Operation)
	if err != nil {
		logger.Error(err, "处理Service失败")
		return NewAdmissionResponse(req.UID).Deny(err.Error()).AdmissionResponse
	}

	if !mutation.Allowed {
		logger.Info("Service被拒绝", "reason", mutation.Message)
		return NewAdmissionResponse(req.UID).Deny(mutation.Message).AdmissionResponse
	}

	// 构建响应
	response := NewAdmissionResponse(req.UID).Allow()

	if len(mutation.Patches) > 0 {
		response.WithPatches(mutation.Patches)
		logger.Info("应用变更补丁", "patches", len(mutation.Patches))
	}

	if len(mutation.Warnings) > 0 {
		response.WithWarnings(mutation.Warnings)
		logger.Info("添加警告信息", "warnings", len(mutation.Warnings))
	}

	logger.Info("准入请求处理完成")
	return response.AdmissionResponse
}

// processService 处理Service的端口分配和验证
func (m *Mutator) processService(ctx context.Context, service *corev1.Service, operation admissionv1.Operation) (*ServiceMutation, error) {
	mutation := &ServiceMutation{
		Service: service,
		Allowed: true,
	}

	// 检查是否有需要分配的端口
	needsAllocation := false
	for _, port := range service.Spec.Ports {
		if port.NodePort == 0 {
			needsAllocation = true
			break
		}
	}

	if needsAllocation || operation == admissionv1.Create {
		return m.handlePortAllocation(ctx, mutation)
	}

	if operation == admissionv1.Update {
		return m.handlePortValidation(ctx, mutation)
	}

	return mutation, nil
}

// handlePortAllocation 处理端口分配
func (m *Mutator) handlePortAllocation(ctx context.Context, mutation *ServiceMutation) (*ServiceMutation, error) {
	allocator := m.portManager.GetAllocator()

	results, err := allocator.AllocateForService(ctx, mutation.Service)
	if err != nil {
		mutation.Allowed = false
		mutation.Message = err.Error()
		return mutation, nil
	}

	// 生成补丁和警告信息
	for _, result := range results {
		if mutation.Service.Spec.Ports[result.PortIndex].NodePort == 0 {
			// 添加补丁来设置NodePort
			patch := MutationPatch{
				Op:    "replace",
				Path:  fmt.Sprintf("/spec/ports/%d/nodePort", result.PortIndex),
				Value: result.AllocatedPort,
			}
			mutation.Patches = append(mutation.Patches, patch)
		}

		// 添加警告信息
		mutation.Warnings = append(mutation.Warnings, result.Message)
	}

	m.logger.Info("端口分配完成",
		"service", fmt.Sprintf("%s/%s", mutation.Service.Namespace, mutation.Service.Name),
		"allocated", len(results))

	return mutation, nil
}

// handlePortValidation 处理端口验证
func (m *Mutator) handlePortValidation(ctx context.Context, mutation *ServiceMutation) (*ServiceMutation, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	
	namespace := mutation.Service.Namespace
	if namespace == "" {
		namespace = "default"
	}

	// 验证所有指定的端口
	for _, port := range mutation.Service.Spec.Ports {
		if port.NodePort != 0 {
			if err := m.portManager.ValidatePortForService(namespace, mutation.Service.Labels, port.NodePort); err != nil {
				mutation.Allowed = false
				mutation.Message = fmt.Sprintf("端口 %d 验证失败: %v", port.NodePort, err)
				return mutation, nil
			}
		}
	}

	return mutation, nil
}

// ServeHTTP 实现http.Handler接口
func (m *Mutator) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "只支持POST方法", http.StatusMethodNotAllowed)
		return
	}

	if r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "Content-Type必须为application/json", http.StatusBadRequest)
		return
	}

	var admissionReview admissionv1.AdmissionReview
	if err := json.NewDecoder(r.Body).Decode(&admissionReview); err != nil {
		m.logger.Error(err, "解析AdmissionReview失败")
		http.Error(w, fmt.Sprintf("解析请求失败: %v", err), http.StatusBadRequest)
		return
	}

	if admissionReview.Request == nil {
		http.Error(w, "AdmissionRequest为空", http.StatusBadRequest)
		return
	}

	// 处理准入请求
	response := m.Handle(r.Context(), admissionReview.Request)

	// 构建响应
	admissionReview.Response = response
	admissionReview.Response.UID = admissionReview.Request.UID

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(admissionReview); err != nil {
		m.logger.Error(err, "编码响应失败")
		http.Error(w, fmt.Sprintf("编码响应失败: %v", err), http.StatusInternalServerError)
	}
}
