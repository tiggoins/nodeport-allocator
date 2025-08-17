package admission

import (
	"encoding/json"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)


// AdmissionResponse 准入响应
type AdmissionResponse struct {
    *admissionv1.AdmissionResponse
}

// MutationPatch JSON Patch操作
type MutationPatch struct {
    Op    string      `json:"op"`
    Path  string      `json:"path"`
    Value interface{} `json:"value,omitempty"`
}

// ServiceMutation Service变更信息
type ServiceMutation struct {
    Service     *corev1.Service
    Patches     []MutationPatch
    Warnings    []string
    Message     string
    Allowed     bool
}

// NewAdmissionResponse 创建准入响应
func NewAdmissionResponse(uid types.UID) *AdmissionResponse {
    return &AdmissionResponse{
        AdmissionResponse: &admissionv1.AdmissionResponse{
            UID:     uid,
            Allowed: true,
            Result:  &metav1.Status{},
        },
    }
}

// Allow 允许请求
func (r *AdmissionResponse) Allow() *AdmissionResponse {
    r.Allowed = true
    return r
}

// Deny 拒绝请求
func (r *AdmissionResponse) Deny(message string) *AdmissionResponse {
    r.Allowed = false
    r.Result = &metav1.Status{
        Code:    403,
        Message: message,
    }
    return r
}

// WithPatches 添加变更补丁
func (r *AdmissionResponse) WithPatches(patches []MutationPatch) *AdmissionResponse {
    if len(patches) > 0 {
        patchBytes, _ := json.Marshal(patches)
        patchType := admissionv1.PatchTypeJSONPatch
        r.Patch = patchBytes
        r.PatchType = &patchType
    }
    return r
}

// WithWarnings 添加警告信息
func (r *AdmissionResponse) WithWarnings(warnings []string) *AdmissionResponse {
    r.Warnings = warnings
    return r
}
