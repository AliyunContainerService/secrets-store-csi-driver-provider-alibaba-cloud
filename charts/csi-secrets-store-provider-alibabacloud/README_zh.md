> 本文档是 [English](README.md) 的中文版本。

# 阿里云密钥管理服务 - Secret Store CSI Driver 提供商

阿里云密钥管理服务提供商用于 Secrets Store CSI driver，可获取存储在[阿里云密钥管理服务（KMS Secrets Manager）](https://www.alibabacloud.com/help/zh/key-management-service/latest/secrets-manager-overview)或[阿里云 OOS 加密参数](https://www.alibabacloud.com/help/zh/oos/getting-started/manage-encryption-parameters)中的密钥内容，并使用 Secrets Store CSI driver 接口将其挂载到 Kubernetes Pod 中。

## 目录

- [前提条件](#前提条件)
- [安装 Chart](#安装-chart)
- [配置参数](#配置参数)
- [使用方式](#使用方式)
- [认证方式说明](#认证方式说明)
  - [Pod SA RRSA（推荐）](#pod-sa-rrsa推荐)
  - [Provider RRSA](#provider-rrsa)
  - [RAM Role ARN](#ram-role-arn)
  - [Node Publish Secret](#node-publish-secret)
  - [AK/SK](#aksk)
  - [ECS RAM Role](#ecs-ram-role)
- [高级用法](#高级用法)
  - [跨账号 KMS 访问](#跨账号-kms-访问)
  - [资源清理](#资源清理)
  - [JMESPath JSON 解析](#jmespath-json-解析)
  - [密钥轮转](#密钥轮转)
  - [Kubernetes Secret 同步](#kubernetes-secret-同步)
- [故障排查](#故障排查)
- [SecretProviderClass 选项](#secretproviderclass-选项)
- [其他注意事项](#其他注意事项)
- [安全](#安全)
- [许可证](#许可证)

## 前提条件

- [Helm3](https://helm.sh/docs/intro/quickstart/#install-helm)
- Kubernetes >= 1.30.0

## 安装 Chart

- 此 Chart 会安装 [secrets-store-csi-driver](https://github.com/kubernetes-sigs/secrets-store-csi-driver) 以及阿里云 KMS 密钥管理服务或 OOS 加密参数的 Provider

```shell
helm repo add csi-secrets-store-provider-alibabacloud https://raw.githubusercontent.com/AliyunContainerService/secrets-store-csi-driver-provider-alibaba-cloud/main/charts

helm install csi-secrets-store-provider-alibabacloud/csi-secrets-store-provider-alibabacloud --generate-name
```

## 配置参数

下表列出了 csi-secrets-store-provider-alibabacloud Chart 的可配置参数及其默认值。

> 关于 secrets-store-csi-driver Chart 的可配置参数，请参考此[文档](https://github.com/kubernetes-sigs/secrets-store-csi-driver/tree/master/charts/secrets-store-csi-driver/README.md)。


| 参数                                                           | 描述                                                                                                                                                                                  | 默认值                                                                                          |
| -------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------- |
| `nameOverride`                                                 | 部分覆盖 csi-secrets-store-provider-alibabacloud.fullname 模板的字符串（将追加 Release 名称作为前缀）                                                                                 | `""`                                                                                            |
| `fullnameOverride`                                             | 完全覆盖 csi-secrets-store-provider-alibabacloud.fullname 模板的字符串                                                                                                                | `""`                                                                                            |
| `imagePullSecrets`                                             | 拉取镜像时使用的 Secret                                                                                                                                                               | `[]`                                                                                            |
| `logFormatJSON`                                                | 使用 JSON 日志格式                                                                                                                                                                    | `false`                                                                                         |
| `logVerbosity`                                                 | 日志级别。使用 V logs (klog)                                                                                                                                                          | `0`                                                                                             |
| `regionId`                                                     | 从指定地域拉取密钥凭证                                                                                                                                                                | `cn-hangzhou`                                                                                   |
| `envVarsFromSecret.ACCESS_KEY_ID`                              | 设置 ACCESS_KEY_ID 变量，指定用于构建 SDK 客户端的 RAM AK，需在名为**alibaba-credentials** 的 Secret 中定义                                                                           |                                                                                                 |
| `envVarsFromSecret.SECRET_ACCESS_KEY`                          | 设置 SECRET_ACCESS_KEY 变量，指定用于构建 SDK 客户端的 RAM SK，需在名为**alibaba-credentials** 的 Secret 中定义                                                                       |                                                                                                 |
| `envVarsFromSecret.ALICLOUD_ROLE_ARN`                          | 设置 ALICLOUD_ROLE_ARN 变量，指定用于构建 SDK 客户端的 RAM Role ARN，需在名为**alibaba-credentials** 的 Secret 中定义                                                                 |                                                                                                 |
| `envVarsFromSecret.ALICLOUD_ROLE_SESSION_NAME`                 | 设置 ALICLOUD_ROLE_SESSION_NAME 变量，指定用于构建 SDK 客户端的 RAM Role Session 名称，需在名为**alibaba-credentials** 的 Secret 中定义                                               |                                                                                                 |
| `envVarsFromSecret.ALICLOUD_ROLE_SESSION_EXPIRATION`           | 设置 ALICLOUD_ROLE_SESSION_EXPIRATION 变量，指定用于构建 SDK 客户端的 RAM Role Session 过期时间，需在名为**alibaba-credentials** 的 Secret 中定义                                           |                                                                                                 |
| `envVarsFromSecret.ALICLOUD_OIDC_PROVIDER_ARN`                 | 设置 ALICLOUD_OIDC_PROVIDER_ARN 变量，指定用于构建 SDK 客户端的 RAM OIDC Provider ARN。Secret 中的 data key 必须为`oidcproviderarn`，需在名为**alibaba-credentials** 的 Secret 中定义 |                                                                                                 |
| `envVarsFromSecret.ALICLOUD_OIDC_TOKEN_FILE`                   | 设置 ALICLOUD_OIDC_TOKEN_FILE 变量，指定用于构建 SDK 客户端的 ServiceAccount OIDC Token 文件路径，需在名为**alibaba-credentials** 的 Secret 中定义                                    |                                                                                                 |
| `rrsa.enable`                                                  | 启用 RRSA 功能（alpha），默认为 false。启用后需通过`rrsa.accountId`/`rrsa.clusterId` 自动构造 OIDC Provider ARN，或通过 `envVarsFromSecret`（Map 结构）显式配置 `ALICLOUD_OIDC_PROVIDER_ARN`   | false                                                                                           |
| `rrsa.accountId`                                               | （可选）设置阿里云账号 ID 以自动构造 OIDC Provider ARN。格式：`acs:ram::<accountId>:oidc-provider/ack-rrsa-<clusterId>`                                                               | `""`                                                                                            |
| `rrsa.clusterId`                                               | （可选）设置 ACK 集群 ID 以自动构造 OIDC Provider ARN。与 rrsa.accountId 配合使用                                                                                                     | `""`                                                                                            |
| `linux.enabled`                                                | 在 Linux 节点上安装 Alibaba Cloud Provider                                                                                                                                            | true                                                                                            |
| `linux.image.repository`                                       | Linux 镜像仓库                                                                                                                                                                        | `registry.cn-hangzhou.aliyuncs.com/acs/secrets-store-csi-driver-provider-alibaba-cloud`         |
| `linux.image.pullPolicy`                                       | Linux 镜像拉取策略                                                                                                                                                                    | `Always`                                                                                        |
| `linux.image.tag`                                              | 阿里云密钥管理服务 Provider Linux 镜像标签                                                                                                                                            | `v0.6.0`                                                                                        |
| `linux.nodeSelector`                                           | Linux 节点 DaemonSet 的节点选择器                                                                                                                                                     | `{}`                                                                                            |
| `linux.tolerations`                                            | Linux 节点 DaemonSet 的容忍度配置                                                                                                                                                     | `[]`                                                                                            |
| `linux.resources`                                              | Linux 节点 Provider Pod 的资源限制                                                                                                                                                    | `requests.cpu: 50m<br>``requests.memory: 100Mi<br>``limits.cpu: 100m<br>``limits.memory: 500Mi` |
| `linux.podLabels`                                              | 额外的 Pod 标签                                                                                                                                                                       | `{}`                                                                                            |
| `linux.podAnnotations`                                         | 额外的 Pod 注解                                                                                                                                                                       | `{}`                                                                                            |
| `linux.priorityClassName`                                      | 表示 Pod 相对于其他 Pod 的优先级                                                                                                                                                      | `""`                                                                                            |
| `linux.updateStrategy`                                         | 配置 Linux 节点 DaemonSet 的自定义更新策略                                                                                                                                            | `RollingUpdate with 1 maxUnavailable`                                                           |
| `linux.healthzPort`                                            | 健康检查端口                                                                                                                                                                          | `"8989"`                                                                                        |
| `linux.healthzPath`                                            | 健康检查路径                                                                                                                                                                          | `"/healthz"`                                                                                    |
| `linux.healthzTimeout`                                         | 健康检查 RPC 超时时间                                                                                                                                                                 | `"5s"`                                                                                          |
| `linux.volumes`                                                | Provider Pod 的额外卷                                                                                                                                                                 | `[]`                                                                                            |
| `linux.volumeMounts`                                           | Provider Pod 的额外卷挂载                                                                                                                                                             | `[]`                                                                                            |
| `linux.affinity`                                               | Linux 节点 Provider Pod 的亲和性配置                                                                                                                                                  | 匹配表达式`type NotIn virtual-kubelet`                                                          |
| `linux.kubeletRootDir`                                         | 配置 kubelet 根目录                                                                                                                                                                   | `/var/lib/kubelet`                                                                              |
| `linux.providersDir`                                           | 配置 Provider 根目录                                                                                                                                                                  | `/var/run/secrets-store-csi-providers`                                                          |
| `secrets-store-csi-driver.install`                             | 随此 Chart 一起安装 secrets-store-csi-driver                                                                                                                                          | true                                                                                            |
| `secrets-store-csi-driver.fullnameOverride`                    | 完全覆盖 secrets-store-csi-driver.fullname 模板的字符串                                                                                                                               | `secrets-store-csi-driver`                                                                      |
| `secrets-store-csi-driver.linux.enabled`                       | 在 Linux 节点上安装 secrets-store-csi-driver                                                                                                                                          | true                                                                                            |
| `secrets-store-csi-driver.linux.crds.image.repository`         | CRDs 安装镜像仓库                                                                                                                                                                     | `registry.k8s.io/csi-secrets-store/driver-crds`                                                 |
| `secrets-store-csi-driver.linux.crds.image.tag`                | CRDs 安装镜像标签                                                                                                                                                                     | `v1.6.0`                                                                                        |
| `secrets-store-csi-driver.linux.image.repository`              | Driver Linux 镜像仓库                                                                                                                                                                 | ` registry.cn-hangzhou.aliyuncs.com/acs/csi-secrets-store-driver`                               |
| `secrets-store-csi-driver.linux.image.pullPolicy`              | Driver Linux 镜像拉取策略                                                                                                                                                             | `Always`                                                                                        |
| `secrets-store-csi-driver.linux.image.tag`                     | Driver Linux 镜像标签                                                                                                                                                                 | `v1.6.0`                                                                                        |
| `secrets-store-csi-driver.linux.livenessProbeImage.repository` | Linux liveness-probe 镜像仓库                                                                                                                                                         | `registry.cn-hangzhou.aliyuncs.com/acs/csi-secrets-store-livenessprobe`                         |
| `secrets-store-csi-driver.linux.livenessProbeImage.pullPolicy` | Linux liveness-probe 镜像拉取策略                                                                                                                                                     | `Always`                                                                                        |
| `secrets-store-csi-driver.linux.livenessProbeImage.tag`        | Linux liveness-probe 镜像标签                                                                                                                                                         | `v2.18.0`                                                                                       |
| `secrets-store-csi-driver.linux.registrarImage.repository`     | Linux node-driver-registrar 镜像仓库                                                                                                                                                  | `registry.cn-hangzhou.aliyuncs.com/acs/csi-node-driver-registrar`                               |
| `secrets-store-csi-driver.linux.registrarImage.pullPolicy`     | Linux node-driver-registrar 镜像拉取策略                                                                                                                                              | `Always`                                                                                        |
| `secrets-store-csi-driver.linux.registrarImage.tag`            | Linux node-driver-registrar 镜像标签                                                                                                                                                  | `v2.16.0`                                                                                       |
| `secrets-store-csi-driver.enableSecretRotation`                | 启用密钥轮转功能 [alpha]                                                                                                                                                              | `false`                                                                                         |
| `secrets-store-csi-driver.rotationPollInterval`                | 密钥轮询间隔时间                                                                                                                                                                      | `2m`                                                                                            |
| `secrets-store-csi-driver.syncSecret.enabled`                  | 启用同步到 Kubernetes 原生 Secret 所需的 RBAC 角色和绑定                                                                                                                              | `false`                                                                                         |
| `secrets-store-csi-driver.tokenRequests`                       | 配置 CSI Driver 请求的 ServiceAccount token audience，用于 Pod SA RRSA 认证                                                                                                           | `[{audience: "sts.aliyuncs.com"}]`                                                              |
| `rbac.install`                                                 | 安装默认 ServiceAccount                                                                                                                                                               | true                                                                                            |

## 使用方式

> **注意**：本节提供 Pod SA RRSA 认证（**推荐**方式）的分步指南。如需了解其他认证方式，请参见[认证方式说明](#认证方式说明)。

### 步骤 1：启用 RRSA

使用 [ack-ram-tool](https://github.com/AliyunContainerService/ack-ram-tool) 在 ACK 集群上启用 [RRSA](https://www.alibabacloud.com/help/zh/container-service-for-kubernetes/latest/use-rrsa-to-enforce-access-control#section-ywl-59g-j8h)（RAM Roles for Service Accounts）：

```shell
ack-ram-tool rrsa enable -c <clusterId>
```

### 步骤 2：创建 KMS 密钥 / OOS 加密参数并创建 RAM 策略

创建密钥数据并创建最小化 RAM 策略以授予读取权限。

**选项 A：KMS 密钥管理服务**

- 使用 aliyun CLI 工具将密钥数据添加到[阿里云密钥管理服务](https://www.alibabacloud.com/help/zh/key-management-service/latest/secrets-manager-overview)，首先使用 `aliyun configure` 命令设置凭证和地域信息，然后使用以下命令创建测试密钥：

  ```shell
  aliyun kms CreateSecret --SecretName test-kms --SecretData 1234 --VersionId v1 --EncryptionKeyId <kms-key-id> --DKMSInstanceId <kms-instance-id> 
  ```
- 使用以下模板创建最小化 RAM 策略：

  ```shell
  aliyun ram CreatePolicy --PolicyName kms-test --PolicyDocument '{
    "Version": "1",
    "Statement": [
      {
        "Effect": "Allow",
        "Action": "kms:GetSecretValue",
        "Resource": "acs:kms:cn-hangzhou:{accountId}:secret/test-kms"
      },
      {
        "Effect": "Allow",
        "Action": "kms:Decrypt",
        "Resource": "acs:kms:cn-hangzhou:{accountId}:key/{kms-key-id}"
      }
    ]}'
  ```

**选项 B：OOS 加密参数**

- 使用 aliyun CLI 工具将密钥数据添加到[阿里云 OOS 加密参数](https://www.alibabacloud.com/help/zh/oos/getting-started/manage-encryption-parameters)，首先使用 `aliyun configure` 命令设置凭证和地域信息，然后使用以下命令创建测试参数：

  ```shell
  aliyun oos CreateSecretParameter --Value SecretParameter --Name test-oos
  ```
- 使用以下模板创建最小化 RAM 策略：

  ```shell
  aliyun ram CreatePolicy --PolicyName oos-test --PolicyDocument '{
  "Version": "1",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "oos:GetSecretParameter",
        "kms:GetSecretValue"
      ],
      "Resource": "acs:oos:cn-hangzhou:{accountId}:secretparameter/test-oos"  # test-oos 是上面创建的参数名称
    }
  ]}'
  ```

### 步骤 3：创建 RAM Role 并配置信任策略

创建 RAM Role，配置信任策略允许 RRSA OIDC Provider 扮演该角色，然后附加步骤 2 中创建的策略：

```bash
# 创建 RAM Role 并配置信任策略
aliyun ram CreateRole --RoleName <roleName> --AssumeRolePolicyDocument '{
  "Statement": [{
    "Action": "sts:AssumeRole",
    "Effect": "Allow",
    "Principal": {
      "Federated": "acs:ram::<accountId>:oidc-provider/ack-rrsa-<clusterId>"
    },
    "Condition": {
      "StringEquals": {
        "oidc:iss": ["https://oidc-ack-<region>.oss-<region>.aliyuncs.com/<clusterId>"],
        "oidc:aud": ["sts.aliyuncs.com"],
        "oidc:sub": ["system:serviceaccount:<namespace>:<your-app-service-account>"]
      }
    }
  }],
  "Version": "1"
}'

# 将步骤 2 中的策略附加到 Role
aliyun ram AttachPolicyToRole --PolicyType Custom --PolicyName kms-test --RoleName <roleName>
```

> **注意**：上述示例使用步骤 2 选项 A 中的 `kms-test` 策略。请替换为实际的策略名称。

### 步骤 4：创建 ServiceAccount 并配置注解

> **重要**：ServiceAccount 的 `namespace` 和 `name` 必须与步骤 3 信任策略中 `oidc:sub` 字段的值完全一致。
> 例如，如果信任策略中配置的是 `"oidc:sub": ["system:serviceaccount:default:app-pod-sa"]`，则 ServiceAccount 必须创建在 `default` 命名空间，且名称必须为 `app-pod-sa`。

创建 ServiceAccount 并为其添加 RAM Role ARN 注解：

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: app-pod-sa
  namespace: <namespace>
  annotations:
    # 通过注解指定 RoleArn（Provider 自动检测）
    # 格式：acs:ram::<ACCOUNT_ID>:role/<ROLE_NAME>
    ack.alibabacloud.com/role-arn: "acs:ram::<accountId>:role/<roleName>"
```

或者为已有的 ServiceAccount 添加注解：

```shell
kubectl annotate serviceaccount -n <namespace> <your-app-service-account> ack.alibabacloud.com/role-arn="acs:ram::<accountId>:role/<roleName>"
```

### 步骤 5：配置 OidcProvider ARN

在 Provider DaemonSet 中配置 OIDC Provider ARN。这是所有使用 Pod SA RRSA 的 Pod 共享的**集群级别**配置。

**方式 A：Helm 安装时配置**（推荐）

通过账号 ID 和集群 ID 自动构造 OIDC Provider ARN：

```shell
helm install csi-secrets-store-provider-alibabacloud/csi-secrets-store-provider-alibabacloud --generate-name \
  --set rrsa.accountId=<accountId> \
  --set rrsa.clusterId=<clusterId>
```

或通过 `envVarsFromSecret`（Map 结构）显式指定 OIDC Provider ARN：

```shell
kubectl create secret generic alibaba-credentials -n kube-system \
  --from-literal=oidcproviderarn=acs:ram::<accountId>:oidc-provider/ack-rrsa-<clusterId>
helm install csi-secrets-store-provider-alibabacloud/csi-secrets-store-provider-alibabacloud --generate-name \
  --set envVarsFromSecret.ALICLOUD_OIDC_PROVIDER_ARN.secretKeyRef=alibaba-credentials \
  --set envVarsFromSecret.ALICLOUD_OIDC_PROVIDER_ARN.key=oidcproviderarn
```

**方式 B：更新已有部署**

创建包含 `oidcproviderarn` 键的 Secret，然后在 Helm values 中通过 `envVarsFromSecret` 配置注入到 Provider DaemonSet：

```shell
kubectl create secret generic alibaba-credentials -n kube-system \
  --from-literal=oidcproviderarn=acs:ram::<accountId>:oidc-provider/ack-rrsa-<clusterId> \
  --dry-run=client -o yaml | kubectl apply -f -
```

然后重启 DaemonSet 以加载更新后的 Secret：

```shell
kubectl rollout restart daemonset csi-secrets-store-provider-alibabacloud -n kube-system
```

> **注意**：OIDC Provider ARN 格式为 `acs:ram::<AccountID>:oidc-provider/<ProviderName>`（注意双冒号 `::`）。

> **重要**：创建 Secret 时，Secret 中的 data key **必须**为 `oidcproviderarn`。`envVarsFromSecret` 中的 `key: oidcproviderarn` 直接指向该 Secret data key。示例：
>
> ```shell
> kubectl create secret generic alibaba-credentials -n kube-system \
>   --from-literal=oidcproviderarn=acs:ram::<accountId>:oidc-provider/<ProviderName>
> ```
>
> 在 `envVarsFromSecret` Map 中，`ALICLOUD_OIDC_PROVIDER_ARN.key=oidcproviderarn` 指向的就是这个 Secret data key。

### 步骤 6：创建 SecretProviderClass 并部署 Pod

创建 SecretProviderClass，设置 `usePodServiceAccountToken: "true"` 以启用 Pod SA 认证：

```yaml
apiVersion: secrets-store.csi.x-k8s.io/v1
kind: SecretProviderClass
metadata:
  name: pod-sa-secrets
  namespace: <namespace>
spec:
  provider: alibabacloud
  parameters:
    region: "cn-hangzhou"
    # 启用 Pod SA 认证
    usePodServiceAccountToken: "true"
    objects: |
      - objectName: "test-kms"
        objectType: "kms"
        objectAlias: "test-kms"
```

部署使用上述 ServiceAccount 的 Pod，通过 CSI volume 挂载 SecretProviderClass：

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-app
spec:
  # 将 Pod 绑定到具有 RAM Role 的 ServiceAccount
  serviceAccountName: app-pod-sa
  containers:
  - name: app
    image: nginx:latest
    volumeMounts:
    - name: secrets-store-inline
      mountPath: /mnt/secrets
      readOnly: true
  volumes:
  - name: secrets-store-inline
    csi:
      driver: secrets-store.csi.k8s.io
      readOnly: true
      volumeAttributes:
        secretProviderClass: "pod-sa-secrets"
```

完整示例请参见 [pod-sa-secretproviderclass.yaml](https://github.com/aliyun/secrets-store-csi-driver-provider-alibaba-cloud/blob/master/examples/pod-sa-secretproviderclass.yaml)。

### 验证挂载

```shell
kubectl exec -it my-app -- cat /mnt/secrets/test-kms; echo
kubectl exec -it my-app -- cat /mnt/secrets/test-oos; echo
```

## 认证方式说明

Provider 支持 **6 种认证方式**。这确保了最大的灵活性和向后兼容性。

> **注意**：以下操作（启用 RRSA、创建 RAM Role、RAM Policy 等）可通过阿里云 OpenAPI 或控制台完成。

### 认证方式对比


| 方式                    | 适用场景                        | 复杂度 | 隔离级别 | 跨账号 |
| ----------------------- | ------------------------------- | ------ | -------- | ------ |
| **Pod SA RRSA** ⭐      | Pod 级别权限隔离                | 中     | Pod      | ✅     |
| **Provider RRSA**       | 集群级统一认证                  | 低     | 命名空间 | ✅     |
| **RAM Role**（AK+Role） | 使用 AK/SK 扮演角色             | 中     | 角色     | ✅     |
| **Node Publish Secret** | K8s Secret 中的 AK/SK           | 低     | 集群     | ✅     |
| **AK/SK**               | 静态凭证                        | 低     | 用户     | ✅     |
| **ECS RAM Role**        | 自动凭证获取（Worker RAM Role） | 低     | 节点     | ✅     |

### 如何选择

- **推荐** ⭐：**Pod SA RRSA** — 最安全的认证方式，提供 Pod 级别权限隔离、无需 DaemonSet 凭据、遵循最小权限原则。适用于生产工作负载。
- **备选**：Provider RRSA，适用于集群范围访问，Pod 配置最少（更简单但隔离粒度较粗）
- **传统**：AK/SK，用于向后兼容（不推荐用于生产环境）
- **自动获取**：ECS RAM Role，适用于集群节点已附加 Worker RAM Role 的场景

所有认证方式均支持通过添加单个 `crossAccountRoleArn` 参数实现**跨账号 KMS 访问**。

### Pod SA RRSA（推荐）

Pod ServiceAccount RRSA 认证提供 **Pod 级别的权限隔离**，每个 Pod 通过其使用的 ServiceAccount 注解中指定的 RAM Role 进行 KMS 访问。这是多租户或多环境场景中最安全、最细粒度的方式。

> **命名空间级别权限隔离**：RRSA 是基于命名空间级别的权限隔离机制。每个 ServiceAccount 属于特定命名空间，每个 SA 通过注解绑定不同的 RAM Role。不同命名空间的 Pod 使用不同 SA，从而获得不同的 RAM Role 权限，实现命名空间级别的权限隔离。

#### RoleArn 配置方式

Pod SA 的 RAM Role ARN 通过 ServiceAccount 上的 `ack.alibabacloud.com/role-arn` 注解配置。Provider 自动检测该注解以获取 RoleArn。

#### 配置步骤

完整的配置流程（启用 RRSA、创建 RAM Role、配置 OidcProviderArn、创建 ServiceAccount、部署 Pod 等），请参考上方[使用方式](#使用方式)中的完整 6 步指南。

#### 前提条件

1. ACK 集群已**启用 RRSA**：`ack-ram-tool rrsa enable -c <clusterId>`
2. **OidcProviderArn 已配置**：在 Provider DaemonSet 中通过 `ALICLOUD_OIDC_PROVIDER_ARN` 环境变量配置（集群级别配置，通过 Helm `rrsa.accountId`/`rrsa.clusterId` 自动构造，或通过 `envVarsFromSecret` 显式指定）
3. 已为 Pod ServiceAccount **创建 RAM Role**：
   - 信任策略：允许 OIDC 身份扮演
   - 权限策略：KMS 访问权限

#### 适用场景

- ✅ **多租户应用**：每个租户的 Pod 具有隔离的密钥访问权限
- ✅ **环境分离**：dev/staging/prod Pod 使用不同的 RAM Role
- ✅ **合规要求**：Pod 级别的访问控制和审计追踪
- ✅ **微服务**：每个服务拥有独立的权限范围

完整示例：[pod-sa-secretproviderclass.yaml](https://github.com/aliyun/secrets-store-csi-driver-provider-alibaba-cloud/blob/master/examples/pod-sa-secretproviderclass.yaml)

### Provider RRSA

> **注意**：对于生产工作负载，我们推荐使用 [Pod SA RRSA](#pod-sa-rrsa推荐)替代，它提供更强的 Pod 级别隔离，且无需在 DaemonSet 中配置凭据。

Provider RRSA 认证提供**集群级统一认证**，Provider DaemonSet 使用自身的 ServiceAccount 进行 KMS 访问。所有 Pod 共享 Provider 的权限，管理更简单，但权限隔离粒度较粗。

> **命名空间级别权限隔离**：RRSA 本质上是基于命名空间级别的权限隔离机制。每个 ServiceAccount 属于特定命名空间，通过 SA 注解绑定的 RAM Role 决定权限范围。在 Provider RRSA 中，Provider 的 SA 位于特定命名空间（通常为 `kube-system`），绑定的 RAM Role 定义了所有 Pod 的集群级权限边界。

#### 适用场景

- 集群级统一认证
- 简化 Pod 配置（无需 Pod 级别的 RRSA）
- 命名空间级别的权限隔离
- 快速部署场景

#### 配置步骤

**步骤 1：创建 RAM Role 并配置信任策略**

为 Provider 的 ServiceAccount 创建 RAM Role，配置信任策略允许 RRSA OIDC Provider 扮演该角色：

```bash
aliyun ram CreateRole --RoleName provider-sa-role --AssumeRolePolicyDocument '{
  "Statement": [{
    "Action": "sts:AssumeRole",
    "Effect": "Allow",
    "Principal": {
      "Federated": "acs:ram::<accountId>:oidc-provider/ack-rrsa-<clusterId>"
    },
    "Condition": {
      "StringEquals": {
        "oidc:iss": ["https://oidc-ack-<region>.oss-<region>.aliyuncs.com/<clusterId>"],
        "oidc:aud": ["sts.aliyuncs.com"],
        "oidc:sub": ["system:serviceaccount:kube-system:csi-secrets-store-provider-alibabacloud"]
      }
    }
  }],
  "Version": "1"
}'
```

**步骤 2：授予 RAM Role KMS 权限**

创建并绑定 RAM 策略，授予 KMS 读取权限：

```bash
aliyun ram CreatePolicy --PolicyName provider-kms-read --PolicyDocument '{
  "Version": "1",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["kms:GetSecretValue", "kms:Decrypt"],
      "Resource": "acs:kms:cn-hangzhou:<accountId>:secret/*"
    }
  ]
}'

aliyun ram AttachPolicyToRole --PolicyType Custom --PolicyName provider-kms-read --RoleName provider-sa-role
```

**步骤 3：配置 Helm values.yaml**

```yaml
envVarsFromSecret:
  ALICLOUD_ROLE_ARN:
    secretKeyRef: alibaba-credentials
    key: rolearn

rrsa:
  enable: true
  # 自动构造 OIDC Provider ARN（推荐）
  accountId: "<your-account-id>"
  clusterId: "<your-cluster-id>"
  
  # 或通过 envVarsFromSecret 显式指定 OIDC Provider ARN：
  # （Secret 中的 data key 必须包含 `oidcproviderarn`）
  # envVarsFromSecret:
  #   ALICLOUD_OIDC_PROVIDER_ARN:
  #     secretKeyRef: alibaba-credentials
  #     key: oidcproviderarn
```

部署前创建包含 RoleArn 的 Secret：

```bash
kubectl create secret generic alibaba-credentials -n kube-system \
  --from-literal=rolearn=acs:ram::<accountId>:role/provider-sa-role
```

**步骤 4：部署 Provider**

```bash
helm install csi-secrets-store-provider-alibabacloud/csi-secrets-store-provider-alibabacloud --generate-name
```

**步骤 5：创建 SecretProviderClass（无需特殊配置）**

```yaml
apiVersion: secrets-store.csi.x-k8s.io/v1
kind: SecretProviderClass
metadata:
  name: provider-rrsa-secrets
  namespace: default
spec:
  provider: alibabacloud
  parameters:
    region: "cn-hangzhou"
    # 无需认证相关参数
    objects: |
      - objectName: "my-secret"
        objectType: "kms"
        objectAlias: "my-secret"
```

#### 前提条件

1. ACK 集群已**启用 RRSA**：`ack-ram-tool rrsa enable -c <clusterId>`
2. **Helm 已配置** `rrsa.enable: true` 和凭证信息

#### 优势

- ✅ Pod 配置简单
- ✅ 集中化权限管理

#### 限制

- ❌ 所有 Pod 共享相同权限
- ❌ 无法实现 Pod 级别隔离
- ❌ 需要配置 Provider DaemonSet

完整示例：
- [provider-rrsa-secretproviderclass.yaml](https://github.com/aliyun/secrets-store-csi-driver-provider-alibaba-cloud/blob/master/examples/provider-rrsa-secretproviderclass.yaml) — SecretProviderClass 和 Pod 配置

### RAM Role ARN

RAM Role 认证使用 AK/SK 作为基础凭证，通过 STS AssumeRole API 扮演 RAM Role。此方式提供临时凭证，而非长期使用 AK/SK。

#### 适用场景

- 使用 AK/SK 扮演 RAM Role
- 需要临时凭证替代长期 AK/SK
- 基于现有 AK/SK 的角色访问控制

#### 配置步骤

**步骤 1：创建 RAM Role**

创建 RAM Role，配置信任策略允许 RAM 用户通过 STS AssumeRole 扮演：

```bash
aliyun ram CreateRole --RoleName <role-name> --AssumeRolePolicyDocument '{
  "Statement": [{
    "Action": "sts:AssumeRole",
    "Effect": "Allow",
    "Principal": {
      "RAM": ["acs:ram::<accountId>:root"]
    }
  }],
  "Version": "1"
}'
```

**步骤 2：授予 RAM Role KMS 权限**

创建并绑定 RAM 策略，授予 KMS 读取权限：

```bash
aliyun ram CreatePolicy --PolicyName kms-read-role --PolicyDocument '{
  "Version": "1",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["kms:GetSecretValue", "kms:Decrypt"],
      "Resource": "acs:kms:cn-hangzhou:<accountId>:secret/*"
    }
  ]
}'

aliyun ram AttachPolicyToRole --PolicyType Custom --PolicyName kms-read-role --RoleName <role-name>
```

> **注意**：KMS 权限属于 **RAM Role**，而非 RAM 用户。RAM 用户仅需要 `sts:AssumeRole` 权限。

**步骤 3：授予 RAM 用户 sts:AssumeRole 权限**

确保 RAM 用户具有扮演 Role 的权限：

```bash
# 附加允许 STS AssumeRole 的系统策略
aliyun ram AttachPolicyToUser --PolicyType System --PolicyName AliyunSTSAssumeRoleAccess --UserName <userName>
```

**步骤 4：创建 Kubernetes Secret**

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: alibaba-credentials
  namespace: <namespace>
type: Opaque
data:
  id: <base64-encoded-access-key-id>
  secret: <base64-encoded-access-key-secret>
  rolearn: <base64-encoded-role-arn>
```

**步骤 5：配置 Helm values.yaml**

```yaml
envVarsFromSecret:
  ACCESS_KEY_ID:
    secretKeyRef: alibaba-credentials
    key: id
  SECRET_ACCESS_KEY:
    secretKeyRef: alibaba-credentials
    key: secret
  ALICLOUD_ROLE_ARN:
    secretKeyRef: alibaba-credentials
    key: rolearn
```

**步骤 6：部署 Provider 并创建 SecretProviderClass**

SecretProviderClass 中无需特殊配置。

#### 前提条件

1. 具有 AK/SK 的 **RAM 用户**，且具有 `sts:AssumeRole` 权限
2. **RAM Role**：
   - 信任策略：允许 RAM 用户扮演
   - 权限策略：KMS 访问权限（kms:GetSecretValue、kms:Decrypt）

#### 优势

- ✅ 临时凭证（比静态 AK/SK 更安全）
- ✅ 基于角色的访问控制

#### 限制

- ❌ 需要管理 AK/SK
- ❌ 比 RRSA 更复杂
- ❌ 仍需要 AK/SK 作为基础凭证

完整示例：[ram-role-secretproviderclass.yaml](https://github.com/aliyun/secrets-store-csi-driver-provider-alibaba-cloud/blob/master/examples/ram-role-secretproviderclass.yaml)

### Node Publish Secret

Node Publish Secret 认证将 AK/SK 存储在 Kubernetes Secret 中，通过 CSI Driver 的 `nodePublishSecretRef` 机制传递给 Provider。

#### 适用场景

- AK/SK 存储在 Kubernetes Secret 中
- 通过 CSI Driver 的标准机制传递凭证
- 与现有 K8s Secret 管理集成

#### 配置步骤

**步骤 1：创建 Kubernetes Secret**

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: alibaba-credentials
  namespace: default
type: Opaque
data:
  access_key: <base64-encoded-access-key>
  access_secret: <base64-encoded-access-key-secret>
```

**步骤 2：创建 SecretProviderClass**

```yaml
apiVersion: secrets-store.csi.x-k8s.io/v1
kind: SecretProviderClass
metadata:
  name: node-publish-secret-example
  namespace: default
spec:
  provider: alibabacloud
  parameters:
    region: "cn-hangzhou"
    objects: |
      - objectName: "my-secret"
        objectType: "kms"
```

**步骤 3：创建带 nodePublishSecretRef 的 Pod**

> **重要**：使用 Node Publish Secret 认证时，Pod 的 CSI 卷**必须**显式配置 `nodePublishSecretRef` 以引用包含 AK/SK 的 K8s Secret。这是 CSI Driver 将凭证传递给 Provider 的必要条件。
>
> **要求：**
> - Secret 必须存在于与 Pod **相同的命名空间**
> - Secret 必须包含 `access_key` 和 `access_secret` 字段

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-app
  namespace: default
spec:
  containers:
  - name: app
    image: nginx:latest
    volumeMounts:
    - name: secrets-store-inline
      mountPath: /mnt/secrets
      readOnly: true
  
  volumes:
  - name: secrets-store-inline
    csi:
      driver: secrets-store.csi.k8s.io
      readOnly: true
      volumeAttributes:
        secretProviderClass: "node-publish-secret-example"
      # 必须：引用包含 AK/SK 的 K8s Secret
      nodePublishSecretRef:
        name: alibaba-credentials
```

#### 前提条件

1. 已创建包含 AK/SK 的 **Kubernetes Secret**
2. **AK/SK** 具有 KMS 访问权限
3. Pod CSI 卷中已配置 **nodePublishSecretRef**（与 Pod 同一命名空间）

#### 优势

- ✅ 使用 Kubernetes 原生 Secret 管理
- ✅ 标准 CSI Driver 机制

#### 限制

- ❌ AK/SK 存储在 K8s Secret 中（访问控制取决于 RBAC）
- ❌ 不如 RRSA 安全
- ❌ AK/SK 为长期凭证，不会自动轮转
- ❌ Pod 卷必须显式配置 `nodePublishSecretRef`

完整示例：[node-publish-secretproviderclass.yaml](https://github.com/aliyun/secrets-store-csi-driver-provider-alibaba-cloud/blob/master/examples/node-publish-secretproviderclass.yaml)

### AK/SK

AK/SK 静态凭证认证直接使用长期的 AccessKey ID 和 AccessKey Secret。**由于安全原因，不推荐在生产环境中使用此方式**。

#### 适用场景

- 测试环境
- 快速验证
- 旧系统兼容

#### ⚠️ 安全警告

- **高风险**：AK/SK 可能泄露
- **无隔离**：所有 Pod 共享相同权限
- **长期凭证**：难以轮转
- **建议**：使用 RRSA 或 RAM Role 替代

#### 配置步骤

**步骤 1：创建 Kubernetes Secret**

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: alibaba-credentials
  namespace: <namespace>
type: Opaque
data:
  id: <base64-encoded-access-key-id>
  secret: <base64-encoded-access-key-secret>
```

**步骤 2：配置 Helm values.yaml**

```yaml
envVarsFromSecret:
  ACCESS_KEY_ID:
    secretKeyRef: alibaba-credentials
    key: id
  SECRET_ACCESS_KEY:
    secretKeyRef: alibaba-credentials
    key: secret
```

#### 前提条件

1. 具有 AK/SK 的 **RAM 用户**
2. **RAM 用户**具有 KMS 访问权限

#### 优势

- ✅ 配置简单
- ✅ 无需设置 Role

#### 限制

- ❌ 生产环境**不安全**
- ❌ 无权限隔离
- ❌ 需手动轮转
- ❌ 不支持临时凭证

完整示例：[ak-sk-secretproviderclass.yaml](https://github.com/aliyun/secrets-store-csi-driver-provider-alibaba-cloud/blob/master/examples/ak-sk-secretproviderclass.yaml)

### ECS RAM Role

ECS RAM Role 认证自动从 ECS 实例元数据服务获取临时凭证。在 ACK 集群中，实际使用的是集群的 **Worker RAM Role**（WorkerRole）——即集群工作节点附加的 RAM Role，而非任意 ECS 实例角色。

> **参考文档**：[为集群的 Worker RAM 角色授权](https://help.aliyun.com/zh/ack/product-overview/product-changes-permissions-of-the-worker-ram-role-of-ack-managed-clusters-are-revoked)

#### 适用场景

- 集群工作节点已附加 Worker RAM Role
- 自动凭证轮转
- 无需管理 AK/SK

#### 配置步骤

确保以下条件：

- 集群的 Worker RAM Role 已具有 KMS 访问权限（`kms:GetSecretValue`、`kms:Decrypt`）

#### 前提条件

1. **工作节点**已附加集群的 Worker RAM Role
2. **Worker RAM Role** 具有 KMS 访问权限（`kms:GetSecretValue`、`kms:Decrypt`）
3. **未配置其他认证方式**

#### 优势

- ✅ 自动凭证轮转
- ✅ 无需管理 AK/SK

#### 限制

- ❌ 同一节点上的所有 Pod 共享权限
- ❌ 仅支持节点级别隔离
- ❌ 需要预先设置 Worker RAM Role

完整示例：[ecs-ram-role-secretproviderclass.yaml](https://github.com/aliyun/secrets-store-csi-driver-provider-alibaba-cloud/blob/master/examples/ecs-ram-role-secretproviderclass.yaml)

## 高级用法

### 跨账号 KMS 访问

Provider 支持使用**所有 6 种认证方式**访问另一个阿里云账号中的 KMS 密钥。跨账号访问是统一能力——无论使用哪种认证方式，只需在 SecretProviderClass 中添加 `crossAccountRoleArn` 参数即可。

- ✅ Pod SA RRSA（Pod 级别隔离）
- ✅ Provider RRSA（集群级统一认证）
- ✅ RAM Role（AK/SK + RoleArn）
- ✅ Node Publish Secret（包含 AK/SK 的 K8s Secret）
- ✅ AK/SK（静态凭证）
- ✅ ECS RAM Role（自动凭证获取）

#### 权限要求（最小权限原则）

**源账号（集群所在账号）：**

- ✅ `sts:AssumeRole` 权限（用于扮演目标账号角色）
- ❌ 不需要 KMS 权限

**目标账号（KMS 所在账号）：**

- ✅ 信任策略允许源账号扮演
- ✅ KMS 访问权限
- ❌ 不需要 STS 权限

#### 前提条件

**目标账号（KMS 所在账号）：**

1. 创建具有 KMS 权限的 RAM Role
2. 配置信任策略允许源账号扮演

**源账号（集群所在账号）：**

1. 基础凭证必须具有 `sts:AssumeRole` 权限

#### 设置步骤

**步骤 1：目标账号（KMS 所在账号）**

1. 创建具有 KMS 权限的 RAM Role
2. 配置信任策略：

```json
{
  "Version": "1",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": "sts:AssumeRole",
      "Principal": {
        "RAM": [
          "acs:ram::<SOURCE_ACCOUNT_ID>:root"
        ]
      }
    }
  ]
}
```

3. 授予 KMS 权限：

```json
{
  "Version": "1",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": "kms:GetSecretValue",
      "Resource": "acs:kms:*:<TARGET_ACCOUNT_ID>:secret/*"
    },
    {
      "Effect": "Allow",
      "Action": "kms:Decrypt",
      "Resource": "acs:kms:*:<TARGET_ACCOUNT_ID>:key/*"
    }
  ]
}
```

**步骤 2：源账号（集群所在账号）**

1. 为基础凭证授予 `sts:AssumeRole` 权限（Pod SA Role、Provider SA Role、Worker RAM Role 或 RAM 用户）
2. 在 SecretProviderClass 的每个 object 中添加 `crossAccountRoleArn`：

```yaml
apiVersion: secrets-store.csi.x-k8s.io/v1
kind: SecretProviderClass
metadata:
  name: cross-account-secrets
  namespace: default
spec:
  provider: alibabacloud
  parameters:
    region: "cn-hangzhou"
    objects: |
      - objectName: "my-secret"
        objectType: "kms"
        objectAlias: "my-secret-alias"
        crossAccountRoleArn: "acs:ram::<TARGET_ACCOUNT_ID>:role/cross-account-kms-role"
```

#### 示例：Pod SA + 跨账号

将 Pod SA 认证与跨账号访问结合，实现最高安全性：

```yaml
apiVersion: secrets-store.csi.x-k8s.io/v1
kind: SecretProviderClass
metadata:
  name: pod-sa-cross-account
  namespace: staging
spec:
  provider: alibabacloud
  parameters:
    region: "cn-hangzhou"
  
    # 启用 Pod SA 认证
    usePodServiceAccountToken: "true"
  
    # 目标账号的 RAM Role（具有 KMS 权限）
    # 此角色必须信任源账号并具有 KMS 访问权限
    objects: |
      - objectName: "staging/app-secret"
        objectType: "kms"
        objectAlias: "app-secret"
        crossAccountRoleArn: "acs:ram::<TARGET_ACCOUNT_ID>:role/cross-account-kms-role"
```

完整示例：[cross-account-secretproviderclass.yaml](https://github.com/aliyun/secrets-store-csi-driver-provider-alibaba-cloud/blob/master/examples/cross-account-secretproviderclass.yaml)

#### 故障排查

**错误："base credential is required for cross-account access"**

- 确保至少配置了一种认证方式（RRSA、Worker RAM Role 或 AK/SK）

**错误："failed to assume cross account role"**

- 验证源账号具有目标角色的 `sts:AssumeRole` 权限
- 检查目标角色的信任策略是否允许源账号
- 验证角色 ARN 格式：`acs:ram::<ACCOUNT_ID>:role/<ROLE_NAME>`

**访问 KMS 时出现 "AccessDenied" 错误**

- 为目标账号角色授予 KMS 权限（不是源账号！）
- 检查 KMS 密钥是否存在于目标账号
- 验证地域是否匹配 KMS 密钥所在地域
- 验证 KMS 密钥 ARN 和地域是否匹配

#### 安全最佳实践

1. **最小权限**：授予最小必要权限
   - 源账号：仅 `sts:AssumeRole` 权限
   - 目标账号：仅必要的 KMS 权限
2. **角色分离**：为不同应用使用不同的 RAM Role
3. **Pod SA 隔离**：优先使用 Pod SA 认证实现 Pod 级别的权限隔离

### JMESPath JSON 解析

Provider 支持使用 [JMESPath](https://jmespath.org/) 表达式从 JSON 格式的密钥中提取特定字段。当单个 KMS 密钥包含多个键值对，且需要将每个键值对挂载为独立文件时，此功能非常有用。

#### 配置

在 SecretProviderClass 的 `objects` 中使用 `jmesPath` 字段指定要提取的字段：

```yaml
# 假设 KMS 密钥 "app-config" 的 JSON 内容为：
# {"username": "admin", "password": "s3cret", "host": "db.example.com"}

apiVersion: secrets-store.csi.x-k8s.io/v1
kind: SecretProviderClass
metadata:
  name: jmespath-example
spec:
  provider: alibabacloud
  parameters:
    region: "cn-hangzhou"
    objects: |
      - objectName: "app-config"
        objectType: "kms"
        jmesPath:
          - path: "username"
            objectAlias: "db-username"
          - path: "password"
            objectAlias: "db-password"
          - path: "host"
            objectAlias: "db-host"
```

这将在挂载路径下挂载三个独立文件：`db-username`、`db-password` 和 `db-host`。

#### 注意事项

- 密钥值必须是有效的 JSON，否则挂载会失败
- 每个 `jmesPath` 条目需要同时指定 `path`（JMESPath 表达式）和 `objectAlias`（输出文件名）
- JMESPath 支持嵌套字段访问，例如 `path: "credentials.username"` 用于嵌套 JSON
- 如果指定的路径在 JSON 中不存在，挂载会失败并报错

### 密钥轮转

Provider 通过 CSI Driver 的 [RequiresRepublish](https://secrets-store-csi-driver.sigs.k8s.io/topics/secret-auto-rotation.html) 机制（v1.6.0+）支持自动密钥轮转。启用后，kubelet 会周期性调用 `NodePublishVolume` 触发重新发布，CSI Driver 在此过程中从 Provider 获取最新的密钥内容。

#### 前提条件

- 必须通过 Helm 启用 CSI Driver 的轮转功能：

  ```bash
  helm upgrade -n <NAMESPACE> csi-secrets-store secrets-store-csi-driver/secrets-store-csi-driver \
    --set enableSecretRotation=true \
    --set rotationPollInterval=1h
  ```

- `requiresRepublish` 是 **CSIDriver** 对象的字段（而非 SecretProviderClass）。当设置 `enableSecretRotation=true` 时，Helm chart 会自动设置该字段，无需手动配置。

#### 工作原理（v1.6.0+）

1. 当 CSIDriver 对象设置了 `requiresRepublish: true` 后，kubelet 会周期性调用 `NodePublishVolume` 重新发布卷
2. 重新发布时，CSI Driver 重新向 Provider 请求密钥内容
3. Provider 从 KMS 获取最新版本的密钥。如果内容已更改，挂载的文件会更新
4. Pod 无需重启即可获取更新后的密钥

> **关于 `rotationPollInterval` 的说明**：在 v1.6.0 中，`rotationPollInterval` 作为**最小缓存间隔**（节流阀）而非精确轮询间隔。实际轮转间隔由 `max(kubelet sync-frequency ≈ 1m, rotationPollInterval)` 决定。例如，如果 `rotationPollInterval=30s`，实际间隔由 kubelet 的 sync-frequency（约 1 分钟）主导；如果 `rotationPollInterval=2h`，实际间隔则约为 2 小时。

### Kubernetes Secret 同步

Provider 支持使用 SecretProviderClass 中的 `secretObjects` 字段将挂载的密钥同步到 Kubernetes 原生 Secret。这允许密钥除了以文件方式使用外，还可以作为环境变量使用。

#### 前提条件

- 必须在 Helm values 中设置 `syncSecret.enabled` 为 `true`（默认：`false`）
- 这会创建 CSI Driver 同步密钥内容所需的 RBAC 角色和绑定

#### 配置

```yaml
apiVersion: secrets-store.csi.x-k8s.io/v1
kind: SecretProviderClass
metadata:
  name: secret-sync-example
spec:
  provider: alibabacloud
  parameters:
    region: "cn-hangzhou"
    objects: |
      - objectName: "my-db-secret"
        objectType: "kms"
        objectAlias: "db-password"
  secretObjects:
    - secretName: k8s-db-secret
      type: Opaque
      data:
        - objectName: db-password   # 必须匹配 parameters 中的 objectAlias
          key: password              # K8s Secret 中的键名
```

#### 同步行为

- 当第一个 Pod 挂载 SecretProviderClass 时，K8s Secret 会自动创建
- 当挂载的密钥内容更改时（如通过轮转），Secret 会更新
- 当使用 SecretProviderClass 的最后一个 Pod 被删除且 SecretProviderClass 本身也被移除时，同步的 K8s Secret 会被 **自动垃圾回收**
- `secretObjects[].data[].objectName` 必须匹配 `parameters.objects` 中的 `objectAlias`（或无别名时的 `objectName`）

#### 注意事项

- 如果未设置 `syncSecret.enabled: true`，`secretObjects` 配置会被静默忽略
- 同步的 K8s Secret 由 CSI Driver 管理；手动修改会被覆盖
- 多个 SecretProviderClass 可以同步到不同的 K8s Secret

#### 资源清理

当使用 SecretProviderClass 的 Pod 被删除时，CSI Driver 会自动清理挂载的密钥文件。如果配置了 `secretObjects` 且 `syncSecret.enabled: true`，则适用以下清理行为：

- **Pod 删除**：只要 SecretProviderClass 存在且可能还有其他 Pod 引用它，同步的 K8s Secret 会保留
- **SecretProviderClass 删除**：当 SecretProviderClass 被删除且没有 Pod 使用它时，同步的 K8s Secret 会被 CSI Driver **自动垃圾回收**
- **命名空间删除**：所有关联的 Secret 和 SecretProviderClass 由 Kubernetes 垃圾回收机制清理

这确保资源移除后集群中不会残留孤立的 Secret。

## 故障排查

大多数错误可以通过描述 Pod 部署来查看。对于部署，使用 get pods 查找 Pod 名称（如果未使用默认命名空间，请使用 -n **&lt;NAMESPACE&gt;**）：

```shell
kubectl get pods
```

然后描述 Pod（将上面的 Pod ID 替换为 **&lt;PODID&gt;**，同样如果未使用默认命名空间请使用 -n）：

```shell
kubectl describe pod/<PODID>
```

更多信息可在 Provider 日志中找到：

```shell
kubectl -n <PROVIDER_NAMESPACE> get pods
kubectl -n <PROVIDER_NAMESPACE> logs pod/<PODID>
```

此处的 **&lt;PODID&gt;** 是 *csi-secrets-store-provider-alibabacloud* Pod 的 ID。

## SecretProviderClass 选项

SecretProviderClass 的格式如下：

```yaml
apiVersion: secrets-store.csi.x-k8s.io/v1
kind: SecretProviderClass
metadata:
  name: <NAME>
spec:
  provider: alibabacloud   # 请使用固定值 'alibabacloud'
  parameters:
```

parameters 部分包含挂载请求的详细信息，包含以下字段之一：

* objects：包含 YAML 声明的字符串（如下所述），描述要挂载的密钥，例如：

  ```yaml
  parameters:
    objects: |
        - objectName: "MySecret"
  ```
* region：可选字段，指定从密钥管理服务检索密钥时使用的阿里云地域。如果缺少此字段，Provider 会查找节点所在的地域。此查找会增加挂载请求的开销，因此使用大量 Pod 的集群可通过在此处指定地域来获得性能提升。
* pathTranslation：可选字段，指定当路径分隔符（Linux 上为斜杠）出现在文件名中时使用的替换字符。如果 Secret 或参数名称包含路径分隔符，Provider 尝试使用该名称创建挂载文件时会失败。未指定时使用下划线，因此 My/Path/Secret 将挂载为 My_Path_Secret。此 pathTranslation 值可以是字符串 "False" 或单个字符字符串。设置为 "False" 时，不进行字符替换。

**认证相关参数：**

* usePodServiceAccountToken：设置为 `"true"` 启用 Pod SA RRSA 认证。启用后，Provider 将使用 Pod 的 ServiceAccount Token 进行认证。默认值：`"false"`。
* crossAccountRoleArn：（可选，每个 object 内配置）目标账号中用于跨账号访问的 RAM Role ARN。在 `parameters.objects` 的每个 object 内配置。格式：`acs:ram::<TARGET_ACCOUNT_ID>:role/<ROLE_NAME>`。指定后，Provider 将在基础认证后扮演此角色。

**同步密钥到 Kubernetes 原生 Secret：**

* 要在 SecretProviderClass 中使用 `secretObjects` 将密钥同步到 K8s Secret，必须在 Helm values 中启用 `syncSecret.enabled: true`。这会创建 CSI Driver 同步密钥内容所需的 RBAC 角色和绑定。如果未设置此项，`secretObjects` 配置将被忽略。

SecretProviderClass 的 objects 字段可包含以下子字段：

* objectName：必填字段。指定要获取的密钥或参数的名称。对于密钥管理服务，这是 [SecretName](https://www.alibabacloud.com/help/zh/key-management-service/latest/getsecretvalue#parameters) 参数，可以是密钥的友好名称或完整 ARN。
* objectType：可选字段。指定密钥类型。支持 `kms` 和 `oos`，默认为 `kms`。
* objectAlias：可选字段。指定密钥挂载时的文件名。未指定时文件名默认为 objectName。
* objectVersion：可选字段，仅适用于 KMS 密钥，通常不推荐使用，因为密钥更新时需要更新此字段。对于密钥管理服务，这是 [VersionId](https://www.alibabacloud.com/help/zh/key-management-service/latest/getsecretvalue#parameters)。
* objectVersionLabel：可选字段，指定版本使用的别名，仅适用于 KMS 密钥。大多数应用不应使用此字段，因为默认使用最新版本的密钥。对于密钥管理服务，这是 [VersionStage](https://www.alibabacloud.com/help/zh/key-management-service/latest/getsecretvalue#parameters)。
* jmesPath：可选字段。指定从 JSON 格式密钥中提取的特定键值对。可使用此字段将格式正确的密钥值中的键值对作为独立密钥挂载。例如：假设密钥 "test" 的 JSON 内容如下：

  ```shell
  {
    "username": "testuser",
    "password": "testpassword"
  }
  ```

  要将此密钥的 username 和 password 键值对作为独立密钥挂载，使用 jmesPath 字段如下：

  ```yaml:
  objects: |
      - objectName: "test"
        jmesPath:
            - path: "username"
              objectAlias: "MySecretUsername"
            - path: "password"
              objectAlias: "MySecretPassword"
  ```

  如果使用 jmesPath 字段，必须提供以下两个子字段：

  * path：必填字段，用于检索的 [JMES 路径](https://jmespath.org/specification.html)
  * objectAlias：必填字段，指定键值对密钥挂载时的文件名
* kmsEndpoint：可选字段，用于指定 KMS 密钥管理服务的访问端点。如未指定，默认端点为 `kms-vpc.{.region-id}.aliyuncs.com`。

  kmsEndpoint 配置说明：KMS 目前支持两种访问方式：专属网关和共享网关。应用支持两种方式，不同方式需要配置不同的 Endpoint。以下是不同网关的 Endpoint 地址配置：


  | 网关类型     | Endpoint 地址                                    | 使用说明                                                                                                                                    |
  | ------------ | ------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------- |
  | 专属网关     | {kms-instance-id}.cryptoservice.kms.aliyuncs.com | 1. 需要 KMS 实例与集群在同一地域和 VPC 中。<br />2. 将 {kms-instance-id} 替换为实际的 KMS 实例 ID。<br />3. KMS 实例版本必须为 3.0 及以上。 |
  | VPC 共享网关 | kms-vpc.{region}.aliyuncs.com                    | 1. 需要 KMS 实例与集群在同一地域。<br />2. 将 {region} 替换为 KMS 实例所在的地域。<br />3. 这是应用的默认配置，无需额外配置。               |
  | 公网共享网关 | kms.{region}.aliyuncs.com                        | 1. 将 {region} 替换为 KMS 实例所在的地域。<br />2. 集群必须具备公网访问能力。                                                               |

**提示**
如果有特殊场景需要相同的 objectName（如下例所示，kms 和 oos 有相同的密钥名称），则需要为对象设置不同的 objectAlias。否则，之前挂载的所有密钥将被最后一个覆盖。

```yaml
parameters:
  objects: |
      - objectName: "MySecret"
        objectType: "kms"
        objectAlias: "MySecretKMS"
      - objectName: "MySecret"
        objectType: "oos"
        objectAlias: "MySecretOOS"
```

## 其他注意事项

### 安全注意事项

本插件旨在确保密钥管理服务与需要从文件系统加载密钥的 Kubernetes 工作负载之间的兼容性。它还支持将这些密钥同步到 Kubernetes 原生 Secret，以便作为环境变量使用。

在评估本插件时，请考虑以下安全威胁：

- 当密钥可通过**文件系统**访问时，[目录遍历](https://en.wikipedia.org/wiki/Directory_traversal_attack)等应用漏洞的严重性可能更高，因为攻击者可能获得读取密钥内容的能力。
- 当密钥通过**环境变量**使用时，错误配置（如启用调试端点或包含记录进程环境详情的依赖）可能泄露密钥。
- 当将密钥内容**同步**到其他数据存储（如 Kubernetes Secret）时，请考虑该数据存储的访问控制范围是否足够窄。

基于以上原因，*在可能的情况下*我们建议直接使用阿里云服务 API。

- [密钥管理服务 API](https://www.alibabacloud.com/help/zh/kms/key-management-service/developer-reference/api-getsecretvalue)
- [加密参数 API](https://www.alibabacloud.com/help/zh/oos/developer-reference/api-oos-2019-06-01-getsecretparameter)

## 安全

请通过电子邮件将漏洞报告发送至 **kubernetes-security@service.aliyun.com**。详情请参阅 [SECURITY.md](./SECURITY.md) 文件。

## 许可证

本项目基于 Apache-2.0 许可证授权。
