# NodePort 分配器

基于 Kubernetes MutatingAdmissionWebhook 的 NodePort 端口分配器，为不同命名空间分配不同的端口范围，支持自动分配、端口回收和多副本部署。

## 功能特性

- **基于 Webhook 的端口分配**: 使用 MutatingAdmissionWebhook 自动为 NodePort Service 分配端口
- **命名空间端口范围隔离**: 为不同命名空间配置不同的端口范围
- **自动端口回收**: Service 删除时自动回收 NodePort 到对应的端口池
- **多副本支持**: 支持多副本部署，使用 Leader Election 确保端口回收一致性
- **高效端口查找**: 使用位图（BitSet）算法高效查找未使用的端口
- **用户友好提示**: kubectl apply 时显示端口分配的警告信息
- **持久化存储**: 使用 ConfigMap 存储端口使用状态
- **端口范围动态管理**: 支持端口范围的动态配置和扩容

## 架构设计

### 核心组件

1. **MutatingAdmissionWebhook**: 拦截 Service 创建/更新请求，执行端口分配和验证
2. **Controller**: 监听 Service 删除事件，执行端口回收
3. **PortManager**: 端口管理核心，管理所有端口范围
4. **BitSet**: 高效的位图算法，用于端口分配查找
5. **Leader Election**: 多副本部署时确保端口回收的一致性

### 工作流程

1. **端口分配**: 用户创建 NodePort Service → Webhook 拦截 → 根据命名空间分配端口范围 → 返回分配结果和警告信息
2. **端口验证**: 用户指定 NodePort → Webhook 验证端口是否在允许范围内且未被使用
3. **端口回收**: Service 删除 → Controller 监听到删除事件 → 释放对应端口到端口池

## 快速开始

### 1. 配置文件

```yaml
# config/config.yaml
portRanges:
  production:
    start: 30000
    end: 30999
    namespaces: ["prod", "production"]
    description: "生产环境端口范围"
  
  staging:
    start: 31000
    end: 31499
    namespaces: ["stage", "staging"]
    description: "预发布环境端口范围"
  
  development:
    start: 31500
    end: 31999
    namespaces: ["dev", "development"]
    description: "开发环境端口范围"
  
  default:
    start: 32000
    end: 32767
    namespaces: ["*"]
    description: "默认端口范围"

defaultRange: "default"

storage:
  configMapName: "nodeport-allocator-state"
  configMapNamespace: "kube-system"
  retryAttempts: 3
  retryDelay: "1s"

logLevel: "info"
```

### 2. 构建和部署

```bash
# 生成项目文件
./generate_project.sh

# 构建项目
cd nodeport-allocator
go mod tidy
go build -o bin/nodeport-allocator cmd/main.go

# 创建必要的 RBAC 权限
kubectl apply -f deploy/rbac.yaml

# 部署 Webhook 配置
kubectl apply -f deploy/webhook.yaml

# 部署应用
kubectl apply -f deploy/deployment.yaml
```

### 3. 测试功能

```bash
# 创建测试 Service（自动分配端口）
kubectl apply -f - <<EOF
apiVersion: v1
kind: Service
metadata:
  name: test-service
  namespace: dev
spec:
  type: NodePort
  selector:
    app: test
  ports:
  - port: 80
    targetPort: 8080
    # nodePort 留空，将自动分配
# nodeport-allocator
