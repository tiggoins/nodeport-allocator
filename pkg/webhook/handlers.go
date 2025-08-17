package webhook

import (
    "context"
    "encoding/json"
    "fmt"
    "net/http"

    "github.com/go-logr/logr"
    admissionv1 "k8s.io/api/admission/v1"
)

// AdmissionHandler 准入控制处理器接口
type AdmissionHandler struct {
    Handler AdmissionWebhook
    Logger  logr.Logger
}

// AdmissionWebhook 准入webhook接口
type AdmissionWebhook interface {
    Handle(ctx context.Context, req *admissionv1.AdmissionRequest) *admissionv1.AdmissionResponse
}

// ServeHTTP 实现http.Handler接口
func (h *AdmissionHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    h.Logger.Info("收到Webhook请求", "method", r.Method, "path", r.URL.Path)

    if r.Method != http.MethodPost {
        h.Logger.Info("拒绝非POST请求", "method", r.Method)
        http.Error(w, "只支持POST方法", http.StatusMethodNotAllowed)
        return
    }

    contentType := r.Header.Get("Content-Type")
    if contentType != "application/json" {
        h.Logger.Info("拒绝非JSON请求", "contentType", contentType)
        http.Error(w, "Content-Type必须为application/json", http.StatusBadRequest)
        return
    }

    var admissionReview admissionv1.AdmissionReview
    if err := json.NewDecoder(r.Body).Decode(&admissionReview); err != nil {
        h.Logger.Error(err, "解析AdmissionReview失败")
        http.Error(w, fmt.Sprintf("解析请求失败: %v", err), http.StatusBadRequest)
        return
    }

    if admissionReview.Request == nil {
        h.Logger.Error(fmt.Errorf("AdmissionRequest为空"), "无效的AdmissionReview")
        http.Error(w, "AdmissionRequest为空", http.StatusBadRequest)
        return
    }

    // 记录请求详情
    req := admissionReview.Request
    h.Logger.Info("处理准入请求",
        "uid", req.UID,
        "kind", req.Kind,
        "namespace", req.Namespace,
        "name", req.Name,
        "operation", req.Operation)

    // 处理准入请求
    response := h.Handler.Handle(r.Context(), req)
    if response == nil {
        h.Logger.Error(fmt.Errorf("处理器返回空响应"), "处理准入请求失败")
        http.Error(w, "内部服务器错误", http.StatusInternalServerError)
        return
    }

    // 设置响应UID
    response.UID = req.UID

    // 构建响应
    admissionReview.Response = response
    admissionReview.Request = nil // 清除请求以减少响应大小

    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusOK)

    if err := json.NewEncoder(w).Encode(admissionReview); err != nil {
        h.Logger.Error(err, "编码响应失败")
        // 此时已经写入了状态码，无法再次设置错误响应
        return
    }

    h.Logger.Info("准入请求处理完成",
        "uid", req.UID,
        "allowed", response.Allowed,
        "warnings", len(response.Warnings))
}

