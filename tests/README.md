# 测试指南

## 概述

本测试框架为 Alibaba Cloud Secrets Store CSI Driver Provider 提供核心认证方式的自动化测试能力。

### 测试范围

- **11 个测试用例** (TC-001 到 TC-011，按认证链优先级排序)
- **认证方式**:
  - TC-001: Pod SA RRSA 认证
  - TC-002: Provider RRSA 认证
  - TC-003: RAM Role 认证 (动态创建临时 RAM User + AssumeRole)
  - TC-004: Node Publish Secret 认证 (动态创建临时 RAM User)
  - TC-005: AK/SK 认证 (动态创建临时 RAM User)
  - TC-006: 跨账号认证 (动态创建临时 RAM User)
  - TC-007: ECS RAM Role 认证 (通过 CLUSTER_ID 自动检测 WorkerRole)
- **功能验证**:
  - TC-008: JMESPath JSON 解析
  - TC-009: Secret 轮转 (Rotation)
  - TC-010: K8s Secret 同步 (secretObjects)
  - TC-011: 删除部署后 Secret 清理

## 目录结构

```
tests/
├── scripts/          # 测试脚本
│   ├── run-tests.sh
│   ├── init-env.sh
│   ├── helpers.bash
│   ├── alibabacloud.bats
│   └── alibabacloud-ak.bats
├── fixtures/         # 测试 YAML 模板
│   ├── spc/          # SecretProviderClass 定义
│   ├── pod/          # Pod 定义
│   └── credentials/  # 凭据配置
├── e2e/              # 端到端测试
├── .env.example      # 环境变量模板
└── values.yaml       # Helm values 配置
```

## 测试环境要求

### Kubernetes 集群

- **版本**: >= v1.22 (推荐 v1.36+)
- **类型**: 阿里云 ACK
- **RRSA 功能**: 必须已启用

### 依赖工具

| 工具 | 用途 |
|------|------|
| kubectl | Kubernetes 命令行工具 |
| aliyun CLI | 阿里云命令行工具 |
| Helm | Kubernetes 包管理器 |

## 快速开始

### 1. 配置环境变量

```bash
cd tests

# 复制环境变量模板
cp .env.example .env

# 编辑 .env 文件，填写必需的环境变量
vim .env
```

**必需配置**:
```bash
SOURCE_ACCOUNT_ID=<你的阿里云账号ID>
CLUSTER_ID=<你的ACK集群ID>
```

**可选配置** (根据测试需要):
```bash
# TC-003/TC-004/TC-005 AK/SK 相关测试（源账号凭证，用于 RAM/KMS API 调用）
ALIBABA_ACCESS_KEY_ID=<你的AK>
ALIBABA_ACCESS_KEY_SECRET=<你的SK>

# TC-006 跨账号测试
TARGET_ACCOUNT_ID=<目标账号ID>
# 可选：配置目标账号 AK/SK 后，测试脚本会自动创建 RAM Role、RAM Policy、KMS Secret
TARGET_ACCOUNT_ACCESS_KEY_ID=<目标账号AK>
TARGET_ACCOUNT_ACCESS_KEY_SECRET=<目标账号SK>
# 如果目标账号 KMS 配置
TARGET_ACCOUNT_ENCRYPTION_KEY_ID=<目标账号加密密钥ID>
TARGET_ACCOUNT_DKMS_INSTANCE_ID=<目标账号DKMS实例ID>
TARGET_ACCOUNT_REGION_ID=<目标账号区域>

```

### 2. 加载环境变量

```bash
set -a && source .env && set +a
```

### 3. 运行测试

```bash
./scripts/run-tests.sh
```

### 4. 查看结果

测试完成后会显示彩色结果摘要，并生成详细报告到 `test-logs-*/` 目录。

## 测试用例说明

### TC-001: Pod SA RRSA 认证

**测试目标**: 验证 Pod 级别的 RRSA 认证和权限隔离

**前置条件**:
- SOURCE_ACCOUNT_ID
- CLUSTER_ID

**测试流程**:
1. 创建 KMS Secret
2. 创建 Pod SA RAM Role
3. 创建 ServiceAccount (带注解)
4. 部署测试 Pod
5. 验证 Secret 挂载

**预期结果**: Pod 成功挂载 KMS Secret，使用 Pod SA 的 RAM Role

---

### TC-002: Provider RRSA 认证

**测试目标**: 验证 Provider 级别的 RRSA 认证

**前置条件**:
- SOURCE_ACCOUNT_ID
- CLUSTER_ID
- Provider 已配置 RRSA

**测试流程**:
1. 创建 KMS Secret
2. 创建 Provider SA RAM Role
3. 部署测试 Pod
4. 验证 Secret 挂载

**预期结果**: Pod 成功挂载 KMS Secret

---

### TC-003: RAM Role 认证 (AK/SK + AssumeRole)

**测试目标**: 验证通过动态创建临时 RAM User + AK/SK + RoleArn AssumeRole 方式访问 KMS Secret

**前置条件**:
- SOURCE_ACCOUNT_ID
- CLUSTER_ID
- ALIBABA_ACCESS_KEY_ID
- ALIBABA_ACCESS_KEY_SECRET

**测试流程**:
1. 动态创建临时 RAM User，获取 AK/SK
2. 创建 KMS Secret
3. 配置 AK/SK + RoleArn 凭据
4. 部署测试 Pod
5. 验证 Secret 挂载

**预期结果**: Pod 成功挂载 KMS Secret，通过 AssumeRole 获取临时凭证

---

### TC-004: Node Publish Secret 认证

**测试目标**: 验证通过 nodePublishSecretRef 传递凭证访问 KMS Secret

**前置条件**:
- SOURCE_ACCOUNT_ID
- CLUSTER_ID
- ALIBABA_ACCESS_KEY_ID
- ALIBABA_ACCESS_KEY_SECRET

**测试流程**:
1. 动态创建临时 RAM User，配置 nodePublishSecret
2. 创建 KMS Secret
3. 创建包含 AK/SK 的 K8s Secret
4. 部署测试 Pod (CSI volume 配置 nodePublishSecretRef)
5. 验证 Secret 挂载

**预期结果**: Pod 成功挂载 KMS Secret，通过 nodePublishSecret 传递凭证

---

### TC-005: AK/SK 认证

**测试目标**: 验证 AK/SK 认证方式（向后兼容）

**前置条件**:
- SOURCE_ACCOUNT_ID
- CLUSTER_ID
- ALIBABA_ACCESS_KEY_ID
- ALIBABA_ACCESS_KEY_SECRET

**测试流程**:
1. 动态创建临时 RAM User，获取 AK/SK，配置 DaemonSet
2. 创建 KMS Secret
3. 配置 AK/SK 凭据
4. 部署测试 Pod
5. 验证 Secret 挂载

**预期结果**: Pod 成功挂载 KMS Secret，使用 AK/SK 认证

**安全提示**: AK/SK 认证不推荐用于生产环境

---

### TC-006: 跨账号认证

**测试目标**: 验证跨阿里云账号访问 KMS Secret

**前置条件**:
- SOURCE_ACCOUNT_ID
- CLUSTER_ID
- ALIBABA_ACCESS_KEY_ID（源账号，用于动态创建临时 RAM User）
- ALIBABA_ACCESS_KEY_SECRET
- TARGET_ACCOUNT_ID
- TARGET_ACCOUNT_ACCESS_KEY_ID (可选，用于自动创建资源)
- TARGET_ACCOUNT_ACCESS_KEY_SECRET (可选，用于自动创建资源)
- TARGET_ACCOUNT_ENCRYPTION_KEY_ID (目标账号使用 DKMS 时必需)
- TARGET_ACCOUNT_DKMS_INSTANCE_ID (目标账号使用 DKMS 时必需)
- TARGET_ACCOUNT_REGION_ID (可选，默认 cn-hangzhou)

**目标账号准备**:
- **方式 1 (推荐 - 全自动)**: 配置 `TARGET_ACCOUNT_ACCESS_KEY_ID/SECRET`，测试脚本会自动:
  1. 创建 KMS Secret
  2. 创建 RAM Policy (KMS 访问权限)
  3. 创建 RAM Role (信任策略自动配置为允许源账号)
  4. 绑定 Policy 到 Role
  5. 测试完成后自动清理所有资源
  
- **方式 2 (手动)**: 不配置 AK/SK，需手动在目标账号创建资源
  1. 创建跨账号 RAM Role
  2. 配置信任策略（允许源账号）
  3. 授予 KMS 访问权限
  4. 创建测试 KMS Secret

**测试流程**:
1. 源账号部署测试 Pod
2. Pod 通过 AssumeRole 访问目标账号 KMS
3. 验证 Secret 挂载

**预期结果**: Pod 成功挂载目标账号的 KMS Secret

---

### TC-007: ECS RAM Role 认证（自动化）

**测试目标**: 验证 ECS 实例 RAM Role 回退认证

**前置条件**:
- SOURCE_ACCOUNT_ID
- CLUSTER_ID（通过 CLUSTER_ID 自动检测 WorkerRole）
- Provider 运行在已绑定 RAM Role 的 ECS 实例上

**测试流程**:
1. 通过 CLUSTER_ID 自动查询集群 WorkerRole 并绑定 KMS 策略
2. 创建 KMS Secret
3. 不配置任何认证环境变量
4. 部署测试 Pod
5. 验证 Secret 挂载
6. 测试结束后自动解绑 KMS 策略

**预期结果**: Pod 成功挂载 KMS Secret，通过 ECS 元数据服务获取临时凭证

---

## 跳过特定测试

```bash
# 跳过 TC-005 和 TC-006
SKIP_TESTS=TC-005,TC-006 ./scripts/run-tests.sh
```

## 测试报告

测试报告包含以下信息：

- 测试用例执行结果（通过/失败/跳过）
- 每个测试的执行时间
- 失败原因详情
- 资源清理状态

报告保存在 `test-logs-<timestamp>/` 目录。

## 故障排查

### RRSA 认证失败

**检查项**:
1. SOURCE_ACCOUNT_ID 和 CLUSTER_ID 是否正确
2. Provider 是否已配置 RRSA
3. RAM Role 信任策略是否正确

### AK/SK 测试失败

**检查项**:
1. ALIBABA_ACCESS_KEY_ID 和 SECRET 是否正确
2. RAM 用户是否有 KMS 权限

### 跨账号测试失败

**检查项**:
1. TARGET_ACCOUNT_ID 是否正确
2. 如果配置了 TARGET_ACCOUNT_ACCESS_KEY_ID/SECRET:
   - 目标账号 AK/SK 是否有创建 RAM Role、RAM Policy、KMS Secret 的权限
   - 查看日志确认资源是否成功创建
3. 如果没有配置 AK/SK (手动模式):
   - 目标账号 RAM Role 信任策略是否允许源账号
   - 目标账号 RAM Role 是否有 KMS 权限

## 清理测试资源

测试脚本会自动清理资源。如果测试中断，可以手动清理：

```bash
# 清理测试命名空间
kubectl delete namespace staging

# 清理 KMS Secret
aliyun kms DeleteSecret --SecretName <secret-name>

# 清理 RAM Role
aliyun ram DeleteRole --RoleName <role-name>
```

## 常见问题

### Q: 需要手动创建 KMS Secret 吗？

A: 不需要，测试脚本会自动创建所有必需的 KMS 和 RAM 资源。

### Q: 测试会修改我的生产环境吗？

A: 不会，所有测试资源都创建在独立的命名空间（默认 staging），测试结束后会自动清理。

### Q: 如何只运行某个测试用例？

A: 使用 SKIP_TESTS 环境变量跳过其他测试：
```bash
SKIP_TESTS=TC-002,TC-003,TC-004,TC-005,TC-006,TC-007 ./scripts/run-tests.sh  # 只运行 TC-001
```

### Q: 跨账号测试失败怎么办？

A: 分两种情况：

**如果使用自动模式 (配置了 TARGET_ACCOUNT_ACCESS_KEY_ID/SECRET)**:
1. 检查目标账号 AK/SK 是否有足够权限
2. 查看日志中的资源创建步骤是否成功
3. 确认源账号 ID 是否正确

**如果使用手动模式 (未配置 AK/SK)**:
1. 创建 RAM Role
2. 信任策略允许源账号：`"Principal": {"RAM": ["acs:ram::<源账号ID>:root"]}`
3. 授予 KMS 访问权限

## 手动测试 (Legacy)

早期的手动测试方式（已废弃）：

```bash
./scripts/init-env.sh --cluster-id xxxxxxx
# Install bats - https://github.com/bats-core/bats-core
KUBECONFIG='/tmp/config' bats scripts/alibabacloud.bats
```

## 版本历史

- **v1.1** (2026-06-09): 按认证链优先级重排测试顺序，新增 TC-003/TC-004/TC-007，扩展到 11 个测试用例
- **v1.0** (2026-06-05): 简化版测试框架，保留 4 个核心测试用例
- **v0.6.0**: 完整版测试框架，7 个测试用例（已废弃）
