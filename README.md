# Alibaba Cloud Secrets Manager for Secret Store CSI Driver

[中文文档](README_zh.md)

Alibaba Cloud Secrets Manager provider for Secrets Store CSI driver allows you to get secret contents stored in [Alibaba Cloud Secrets Manager](https://www.alibabacloud.com/help/en/key-management-service/latest/secrets-manager-overview) or [Alibaba Cloud OOS Encrypted Parameter](https://www.alibabacloud.com/help/en/oos/getting-started/manage-encryption-parameters), and use the Secrets Store CSI driver interface to mount them into Kubernetes pods.

## Table of Contents

- [Prerequisites](#prerequisites)
- [Installing the Chart](#installing-the-chart)
- [Configuration](#configuration)
- [Usage](#usage)
- [Authentication Methods](#authentication-methods)
  - [Pod SA RRSA (Recommended)](#pod-sa-rrsa-recommended)
  - [Provider RRSA](#provider-rrsa)
  - [RAM Role ARN](#ram-role-arn)
  - [Node Publish Secret](#node-publish-secret)
  - [AK/SK](#aksk)
  - [ECS RAM Role](#ecs-ram-role)
- [Advanced Usage](#advanced-usage)
  - [Cross-Account KMS Access](#cross-account-kms-access)
  - [Resource Cleanup](#resource-cleanup)
  - [JMESPath JSON Parsing](#jmespath-json-parsing)
  - [Secret Rotation](#secret-rotation)
  - [Kubernetes Secret Sync](#kubernetes-secret-sync)
- [Troubleshooting](#troubleshooting)
- [SecretProviderClass Options](#secretproviderclass-options)
- [Additional Considerations](#additional-considerations)
- [Security](#security)
- [License](#license)

## Prerequisites

- [Helm3](https://helm.sh/docs/intro/quickstart/#install-helm)
- Kubernetes >= 1.30.0

## Installing the Chart

- This chart installs the [secrets-store-csi-driver](https://github.com/kubernetes-sigs/secrets-store-csi-driver) and the Alibaba Cloud KMS Secrets Manager or OOS Encrypted Parameter provider for the driver

```shell
helm repo add csi-secrets-store-provider-alibabacloud https://raw.githubusercontent.com/AliyunContainerService/secrets-store-csi-driver-provider-alibaba-cloud/main/charts

helm install csi-secrets-store-provider-alibabacloud/csi-secrets-store-provider-alibabacloud --generate-name
```

## Configuration

The following table lists the configurable parameters of the csi-secrets-store-provider-alibabacloud chart and their default values.

> Refer to [doc](https://github.com/kubernetes-sigs/secrets-store-csi-driver/tree/master/charts/secrets-store-csi-driver/README.md) for configurable parameters of the secrets-store-csi-driver chart.

| Parameter                                                        | Description                                                                                                                                                                                          | Default                                                                                           |
| ---------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------- |
| `nameOverride`                                                 | String to partially override csi-secrets-store-provider-alibabacloud.fullname template with a string (will prepend the release name)                                                                 | `""`                                                                                            |
| `fullnameOverride`                                             | String to fully override csi-secrets-store-provider-alibabacloud.fullname template with a string                                                                                                     | `""`                                                                                            |
| `imagePullSecrets`                                             | Secrets to be used when pulling images                                                                                                                                                               | `[]`                                                                                            |
| `logFormatJSON`                                                | Use JSON logging format                                                                                                                                                                              | `false`                                                                                         |
| `logVerbosity`                                                 | Log level. Uses V logs (klog)                                                                                                                                                                        | `0`                                                                                             |
| `regionId`                                                     | Pull secret credentials from the specified region                                                                                                                                                                           | `cn-hangzhou`                                                                                             |
| `envVarsFromSecret.ACCESS_KEY_ID`                              | Set the ACCESS_KEY_ID variable to specify the credential RAM AK for building SDK client, which needs to be defined in the secret named**alibaba-credentials**                                  |                                                                                                   |
| `envVarsFromSecret.SECRET_ACCESS_KEY`                          | Set the SECRET_ACCESS_KEY variable to specify the credential RAM SK for building SDK client, which needs to be defined in the secret named**alibaba-credentials**                              |                                                                                                   |
| `envVarsFromSecret.ALICLOUD_ROLE_ARN`                          | Set the ALICLOUD_ROLE_ARN variable to specify the RAM role ARN for building SDK client, which needs to be defined in the secret named**alibaba-credentials**                                   |                                                                                                   |
| `envVarsFromSecret.ALICLOUD_ROLE_SESSION_NAME`                 | Set the ALICLOUD_ROLE_SESSION_NAME variable to specify the RAM role session name for building SDK client, which needs to be defined in the secret named**alibaba-credentials**                 |                                                                                                   |
| `envVarsFromSecret.ALICLOUD_ROLE_SESSION_EXPIRATION`           | Set the ALICLOUD_ROLE_SESSION_EXPIRATION variable to specify the RAM role session expiration for building SDK client, which needs to be defined in the secret named**alibaba-credentials**           |                                                                                                   |
| `envVarsFromSecret.ALICLOUD_OIDC_PROVIDER_ARN`                 | Set the ALICLOUD_OIDC_PROVIDER_ARN variable to specify the RAM OIDC provider arn for building SDK client. The Secret data key must be `oidcproviderarn`, defined in the secret named**alibaba-credentials**                |                                                                                                   |
| `envVarsFromSecret.ALICLOUD_OIDC_TOKEN_FILE`                   | Set the ALICLOUD_OIDC_TOKEN_FILE variable to specify the serviceaccount OIDC token file path for building SDK client, which needs to be defined in the secret named**alibaba-credentials**     |                                                                                                   |
| `rrsa.enable`                                                  | Enable RRSA feature (alpha), default is false. When enabled, configure OIDC Provider ARN via `rrsa.accountId`/`rrsa.clusterId` (auto-construct) or explicitly via `envVarsFromSecret` (Map structure)                        | false                                                                                             |
| `rrsa.accountId`                                               | (Alternative) Set the Alibaba Cloud Account ID to auto-construct OIDC Provider ARN. Format: `acs:ram::<accountId>:oidc-provider/ack-rrsa-<clusterId>` | `""`                                                                                              |
| `rrsa.clusterId`                                               | (Alternative) Set the ACK Cluster ID to auto-construct OIDC Provider ARN. Used with rrsa.accountId | `""`                                                                                              |
| `linux.enabled`                                                | Install alibabacloud provider on linux nodes                                                                                                                                                         | true                                                                                              |
| `linux.image.repository`                                       | Linux image repository                                                                                                                                                                               | `registry.cn-hangzhou.aliyuncs.com/acs/secrets-store-csi-driver-provider-alibaba-cloud`         |
| `linux.image.pullPolicy`                                       | Linux image pull policy                                                                                                                                                                              | `Always`                                                                                        |
| `linux.image.tag`                                              | Alibaba Cloud Secrets Manager Provider Linux image tag                                                                                                                                               | `v0.6.0`                                                                                        |
| `linux.nodeSelector`                                           | Node Selector for the daemonset on linux nodes                                                                                                                                                       | `{}`                                                                                            |
| `linux.tolerations`                                            | Tolerations for the daemonset on linux nodes                                                                                                                                                         | `[]`                                                                                            |
| `linux.resources`                                              | Resource limit for provider pods on linux nodes                                                                                                                                                      | `requests.cpu: 50m<br>``requests.memory: 100Mi<br>``limits.cpu: 100m<br>``limits.memory: 500Mi` |
| `linux.podLabels`                                              | Additional pod labels                                                                                                                                                                                | `{}`                                                                                            |
| `linux.podAnnotations`                                         | Additional pod annotations                                                                                                                                                                           | `{}`                                                                                            |
| `linux.priorityClassName`                                      | Indicates the importance of a Pod relative to other Pods.                                                                                                                                            | `""`                                                                                            |
| `linux.updateStrategy`                                         | Configure a custom update strategy for the daemonset on linux nodes                                                                                                                                  | `RollingUpdate with 1 maxUnavailable`                                                           |
| `linux.healthzPort`                                            | port for health check                                                                                                                                                                                | `"8989"`                                                                                        |
| `linux.healthzPath`                                            | path for health check                                                                                                                                                                                | `"/healthz"`                                                                                    |
| `linux.healthzTimeout`                                         | RPC timeout for health check                                                                                                                                                                         | `"5s"`                                                                                          |
| `linux.volumes`                                                | Additional volumes to create for the provider pods.                                                                                                                                                  | `[]`                                                                                            |
| `linux.volumeMounts`                                           | Additional volumes to mount on the provider pods.                                                                                                                                                    | `[]`                                                                                            |
| `linux.affinity`                                               | Configures affinity for provider pods on linux nodes                                                                                                                                                 | Match expression `type NotIn virtual-kubelet`                                                   |
| `linux.kubeletRootDir`                                         | Configure the kubelet root dir                                                                                                                                                                       | `/var/lib/kubelet`                                                                              |
| `linux.providersDir`                                           | Configure the providers root dir                                                                                                                                                                     | `/var/run/secrets-store-csi-providers`                                                          |
| `secrets-store-csi-driver.install`                             | Install secrets-store-csi-driver with this chart                                                                                                                                                     | true                                                                                              |
| `secrets-store-csi-driver.fullnameOverride`                    | String to fully override secrets-store-csi-driver.fullname template with a string                                                                                                                    | `secrets-store-csi-driver`                                                                      |
| `secrets-store-csi-driver.linux.enabled`                       | Install secrets-store-csi-driver on linux nodes                                                                                                                                                      | true                                                                                              |
| `secrets-store-csi-driver.linux.crds.image.repository`         | CRDs installation image repository                                                                                                                                                                   | `registry.k8s.io/csi-secrets-store/driver-crds`                                                   |
| `secrets-store-csi-driver.linux.crds.image.tag`                | CRDs installation image tag                                                                                                                                                                          | `v1.6.0`                                                                                        |
| `secrets-store-csi-driver.linux.image.repository`              | Driver Linux image repository                                                                                                                                                                        | ` registry.cn-hangzhou.aliyuncs.com/acs/csi-secrets-store-driver`                               |
| `secrets-store-csi-driver.linux.image.pullPolicy`              | Driver Linux image pull policy                                                                                                                                                                       | `Always`                                                                                        |
| `secrets-store-csi-driver.linux.image.tag`                     | Driver Linux image tag                                                                                                                                                                               | `v1.6.0`                                                                                        |
| `secrets-store-csi-driver.linux.livenessProbeImage.repository` | Linux liveness-probe image repository                                                                                                                                                                | `registry.cn-hangzhou.aliyuncs.com/acs/csi-secrets-store-livenessprobe`                         |
| `secrets-store-csi-driver.linux.livenessProbeImage.pullPolicy` | Linux liveness-probe image pull policy                                                                                                                                                               | `Always`                                                                                        |
| `secrets-store-csi-driver.linux.livenessProbeImage.tag`        | Linux liveness-probe image tag                                                                                                                                                                       | `v2.18.0`                                                                                       |
| `secrets-store-csi-driver.linux.registrarImage.repository`     | Linux node-driver-registrar image repository                                                                                                                                                         | `registry.cn-hangzhou.aliyuncs.com/acs/csi-node-driver-registrar`                               |
| `secrets-store-csi-driver.linux.registrarImage.pullPolicy`     | Linux node-driver-registrar image pull policy                                                                                                                                                        | `Always`                                                                                        |
| `secrets-store-csi-driver.linux.registrarImage.tag`            | Linux node-driver-registrar image tag                                                                                                                                                                | `v2.16.0`                                                                                       |
| `secrets-store-csi-driver.enableSecretRotation`                | Enable secret rotation feature [alpha]                                                                                                                                                               | `false`                                                                                         |
| `secrets-store-csi-driver.rotationPollInterval`                | Secret rotation poll interval duration                                                                                                                                                               | `2m`                                                                                            |
| `secrets-store-csi-driver.syncSecret.enabled`                  | Enable rbac roles and bindings required for syncing to Kubernetes native secrets                                                                                                                     | `false`                                                                                         |
| `secrets-store-csi-driver.tokenRequests`                       | Configure ServiceAccount token audience requested by CSI Driver, used for Pod SA RRSA authentication                                                                                                 | `[{audience: "sts.aliyuncs.com"}]`                                                              |
| `rbac.install`                                                 | Install default service account                                                                                                                                                                      | true                                                                                              |

## Usage

> **Note**: This section provides a step-by-step guide for Pod SA RRSA authentication (the **recommended** approach). For other authentication methods, see [Authentication Methods](#authentication-methods).

### Step 1: Enable RRSA

Enable [RRSA](https://www.alibabacloud.com/help/zh/container-service-for-kubernetes/latest/use-rrsa-to-enforce-access-control#section-ywl-59g-j8h) (RAM Roles for Service Accounts) on your ACK cluster using [ack-ram-tool](https://github.com/AliyunContainerService/ack-ram-tool):

```shell
ack-ram-tool rrsa enable -c <clusterId>
```

### Step 2: Create KMS Secret / OOS Parameter and RAM Policy

Create the secret data and a minimized RAM Policy to grant read access.

**Option A: KMS Secrets Manager**

- Add your secret data to [Alibaba Cloud Secrets Manager](https://www.alibabacloud.com/help/en/key-management-service/latest/secrets-manager-overview) with aliyun CLI tool, firstly use `aliyun configure` command to set your credentials and region info, then create a test secret using the following command:

  ```shell
  aliyun kms CreateSecret --SecretName test-kms --SecretData 1234 --VersionId v1 --EncryptionKeyId <kms-key-id> --DKMSInstanceId <kms-instance-id> 
  ```

- Create a minimized RAM policy using the template below:

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

**Option B: OOS Encryption Parameters**

- Add your secret data to [Alibaba Cloud OOS Encrypted Parameter](https://www.alibabacloud.com/help/en/oos/getting-started/manage-encryption-parameters) with aliyun CLI tool, firstly use `aliyun configure` command to set your credentials and region info, then create a test parameter using the following command:

  ```shell
  aliyun oos CreateSecretParameter --Value SecretParameter --Name test-oos
  ```

- Create a minimized RAM policy using the template below:

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
      "Resource": "acs:oos:cn-hangzhou:{accountId}:secretparameter/test-oos"  # test-oos is the name of the parameter created above
    }
  ]}'
  ```

### Step 3: Create RAM Role and Configure Trust Policy

Create a RAM Role with a trust policy that allows the RRSA OIDC provider to assume the role, then attach the policy created in Step 2:

```bash
# Create RAM Role with trust policy
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

# Attach the policy from Step 2 to the Role
aliyun ram AttachPolicyToRole --PolicyType Custom --PolicyName kms-test --RoleName <roleName>
```

> **Note**: The above example uses the `kms-test` policy from Step 2 Option A. Replace with your actual policy name.

### Step 4: Create ServiceAccount and Configure Annotation

> **Important**: The ServiceAccount's `namespace` and `name` must exactly match the value in the `oidc:sub` field from Step 3's trust policy.
> For example, if the trust policy specifies `"oidc:sub": ["system:serviceaccount:default:app-pod-sa"]`, the ServiceAccount must be created in the `default` namespace with the name `app-pod-sa`.

Create the ServiceAccount and annotate it with the RAM Role ARN:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: app-pod-sa
  namespace: <namespace>
  annotations:
    # Specify RoleArn via annotation (auto-detected by Provider)
    # Format: acs:ram::<ACCOUNT_ID>:role/<ROLE_NAME>
    ack.alibabacloud.com/role-arn: "acs:ram::<accountId>:role/<roleName>"
```

Or annotate an existing ServiceAccount:

```shell
kubectl annotate serviceaccount -n <namespace> <your-app-service-account> ack.alibabacloud.com/role-arn="acs:ram::<accountId>:role/<roleName>"
```

### Step 5: Configure OidcProvider ARN

Configure the OIDC Provider ARN in the Provider DaemonSet. This is a **cluster-level** configuration shared by all Pods using Pod SA RRSA.

**Option A: During Helm installation** (recommended)

Auto-construct OIDC Provider ARN from Account ID and Cluster ID:

```shell
helm install csi-secrets-store-provider-alibabacloud/csi-secrets-store-provider-alibabacloud --generate-name \
  --set rrsa.accountId=<accountId> \
  --set rrsa.clusterId=<clusterId>
```

Or explicitly specify OIDC Provider ARN via `envVarsFromSecret` (Map structure):

```shell
kubectl create secret generic alibaba-credentials -n kube-system \
  --from-literal=oidcproviderarn=acs:ram::<accountId>:oidc-provider/ack-rrsa-<clusterId>
helm install csi-secrets-store-provider-alibabacloud/csi-secrets-store-provider-alibabacloud --generate-name \
  --set envVarsFromSecret.ALICLOUD_OIDC_PROVIDER_ARN.secretKeyRef=alibaba-credentials \
  --set envVarsFromSecret.ALICLOUD_OIDC_PROVIDER_ARN.key=oidcproviderarn
```

**Option B: Update existing deployment**

Create a Secret with the `oidcproviderarn` key, then configure `envVarsFromSecret` in Helm values to inject it into the Provider DaemonSet:

```shell
kubectl create secret generic alibaba-credentials -n kube-system \
  --from-literal=oidcproviderarn=acs:ram::<accountId>:oidc-provider/ack-rrsa-<clusterId> \
  --dry-run=client -o yaml | kubectl apply -f -
```

Then restart the DaemonSet to pick up the updated Secret:

```shell
kubectl rollout restart daemonset csi-secrets-store-provider-alibabacloud -n kube-system
```

> **Note**: The OIDC Provider ARN format is `acs:ram::<AccountID>:oidc-provider/<ProviderName>` (note the double colon `::`).

> **Important**: When creating the Secret, the data key **must** be `oidcproviderarn`. The `key: oidcproviderarn` in `envVarsFromSecret` maps directly to this Secret data key. Example:
> ```shell
> kubectl create secret generic alibaba-credentials -n kube-system \
>   --from-literal=oidcproviderarn=acs:ram::<accountId>:oidc-provider/<ProviderName>
> ```
> In the `envVarsFromSecret` Map, `ALICLOUD_OIDC_PROVIDER_ARN.key=oidcproviderarn` points to this exact Secret data key.

### Step 6: Create SecretProviderClass and Deploy Pod

Create a SecretProviderClass with `usePodServiceAccountToken: "true"` to enable Pod SA authentication:

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
    # Enable Pod SA authentication
    usePodServiceAccountToken: "true"
    objects: |
      - objectName: "test-kms"
        objectType: "kms"
        objectAlias: "test-kms"
```

Deploy a Pod that uses the ServiceAccount via `serviceAccountName` and mounts the SecretProviderClass via a CSI volume:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-app
spec:
  # Bind Pod to ServiceAccount with RAM Role
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

See [pod-sa-secretproviderclass.yaml](https://github.com/aliyun/secrets-store-csi-driver-provider-alibaba-cloud/blob/master/examples/pod-sa-secretproviderclass.yaml) for a complete example.

### Verify the Mount

```shell
kubectl exec -it my-app -- cat /mnt/secrets/test-kms; echo
kubectl exec -it my-app -- cat /mnt/secrets/test-oos; echo
```

## Authentication Methods

The provider supports **6 authentication methods**. This ensures maximum flexibility and backward compatibility.

> **Note**: The following operations (enabling RRSA, creating RAM Role, RAM Policy, etc.) can be performed via Alibaba Cloud OpenAPI or Console.

### Authentication Methods Comparison

| Method | Use Case | Complexity | Isolation | Cross-Account |
|--------|----------|------------|-----------|---------------|
| **Pod SA RRSA** ⭐ | Pod-level permission isolation | Medium | Pod | ✅ |
| **Provider RRSA** | Cluster-level unified authentication | Low | Namespace | ✅ |
| **RAM Role** (AK+Role) | AssumeRole with AK/SK | Medium | Role | ✅ |
| **Node Publish Secret** | AK/SK in K8s Secret | Low | Cluster | ✅ |
| **AK/SK** | Static credentials | Low | User | ✅ |
| **ECS RAM Role** | Automatic credential retrieval (Worker RAM Role) | Low | Node | ✅ |

### How to Choose

- **Recommended** ⭐: **Pod SA RRSA** — the most secure approach with Pod-level permission isolation, no DaemonSet credentials, and least-privilege principle. Ideal for production workloads.
- **Alternative**: Provider RRSA for cluster-wide access with minimal Pod configuration (simpler but coarser isolation)
- **Legacy**: AK/SK for backward compatibility (not recommended for production)
- **Automatic**: ECS RAM Role if your cluster's worker nodes already have a Worker RAM Role attached

All authentication methods support **cross-account KMS access** by adding a single `crossAccountRoleArn` parameter.

### Pod SA RRSA (Recommended)

Pod ServiceAccount RRSA authentication provides **Pod-level permission isolation**, where each Pod uses the RAM Role specified in its ServiceAccount annotation for KMS access. This is the most secure and granular approach for multi-tenant or multi-environment scenarios.

> **Namespace-scoped permission isolation**: RRSA is a namespace-level permission isolation mechanism. Each ServiceAccount belongs to a specific namespace, and each SA binds a different RAM Role via annotation. Pods in different namespaces use different SAs, thereby obtaining different RAM Role permissions. This achieves namespace-level permission isolation.

#### RoleArn Configuration

The Pod SA's RAM Role ARN is configured via the `ack.alibabacloud.com/role-arn` annotation on the ServiceAccount. The Provider auto-detects this annotation to obtain the RoleArn.

#### Configuration Steps

For the complete configuration process (enable RRSA, create RAM Role, configure OidcProviderArn, create ServiceAccount, deploy Pod, etc.), please refer to the [Usage](#usage) section above for the full 6-step guide.

#### Prerequisites

1. **RRSA enabled** on ACK cluster: `ack-ram-tool rrsa enable -c <clusterId>`
2. **OidcProviderArn configured** in Provider DaemonSet via `ALICLOUD_OIDC_PROVIDER_ARN` environment variable (cluster-level config, set via Helm `rrsa.accountId`/`rrsa.clusterId` for auto-construction, or via `envVarsFromSecret` for explicit ARN)
3. **RAM Role created** for Pod ServiceAccount with:
   - Trust policy: Allow OIDC identity assumption
   - Permission policy: KMS access permissions

#### Use Cases

- ✅ **Multi-tenant applications**: Each tenant's Pod has isolated secret access
- ✅ **Environment separation**: dev/staging/prod Pods use different RAM Roles
- ✅ **Compliance requirements**: Pod-level access control and audit trail
- ✅ **Microservices**: Each service has its own permission scope

See the complete example: [pod-sa-secretproviderclass.yaml](https://github.com/aliyun/secrets-store-csi-driver-provider-alibaba-cloud/blob/master/examples/pod-sa-secretproviderclass.yaml)

### Provider RRSA

> **Note**: For production workloads, we recommend using [Pod SA RRSA](#pod-sa-rrsa-recommended) instead, which provides stronger Pod-level isolation and does not require credentials in the DaemonSet.

Provider RRSA authentication provides **cluster-level unified authentication**, where the Provider DaemonSet uses its own ServiceAccount for KMS access. All Pods share the Provider's permissions, making it simpler to manage but with coarser permission isolation.

> **Namespace-scoped permission isolation**: RRSA is inherently a namespace-level permission isolation mechanism. Each ServiceAccount belongs to a specific namespace, and the RAM Role bound via SA annotation determines the permission scope. In Provider RRSA, the Provider's SA resides in a specific namespace (typically `kube-system`), and the bound RAM Role defines the cluster-wide permission boundary for all Pods.

#### Use Cases

- Cluster-wide unified authentication
- Simplified Pod configuration (no Pod-level RRSA needed)
- Namespace-level permission isolation
- Rapid deployment scenarios

#### Configuration Steps

**Step 1: Create RAM Role and Configure Trust Policy**

Create a RAM Role for the Provider's ServiceAccount with a trust policy that allows the RRSA OIDC provider to assume the role:

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

**Step 2: Grant KMS Permissions to RAM Role**

Create and attach a RAM Policy that grants KMS read permissions:

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

**Step 3: Configure Helm values.yaml**

```yaml
envVarsFromSecret:
  ALICLOUD_ROLE_ARN:
    secretKeyRef: alibaba-credentials
    key: rolearn

rrsa:
  enable: true
  # Auto-construct OIDC Provider ARN (recommended)
  accountId: "<your-account-id>"
  clusterId: "<your-cluster-id>"
  
  # Or explicitly specify OIDC Provider ARN via envVarsFromSecret:
  # (The Secret data key must contain `oidcproviderarn`)
  # envVarsFromSecret:
  #   ALICLOUD_OIDC_PROVIDER_ARN:
  #     secretKeyRef: alibaba-credentials
  #     key: oidcproviderarn
```

Create the Secret containing the RoleArn before deploying:

```bash
kubectl create secret generic alibaba-credentials -n kube-system \
  --from-literal=rolearn=acs:ram::<accountId>:role/provider-sa-role
```

**Step 4: Deploy Provider**

```bash
helm install csi-secrets-store-provider-alibabacloud/csi-secrets-store-provider-alibabacloud --generate-name
```

**Step 5: Create SecretProviderClass (No Special Config)**

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
    # No authentication-specific parameters needed
    objects: |
      - objectName: "my-secret"
        objectType: "kms"
        objectAlias: "my-secret"
```

#### Prerequisites

1. **RRSA enabled** on ACK cluster: `ack-ram-tool rrsa enable -c <clusterId>`
2. **Helm configured** with `rrsa.enable: true` and credentials

#### Advantages

- ✅ Simple Pod configuration
- ✅ Centralized permission management

#### Limitations

- ❌ All Pods share the same permissions
- ❌ Cannot achieve Pod-level isolation
- ❌ Requires Provider DaemonSet configuration

See the complete examples:
- [provider-rrsa-secretproviderclass.yaml](https://github.com/aliyun/secrets-store-csi-driver-provider-alibaba-cloud/blob/master/examples/provider-rrsa-secretproviderclass.yaml) — SecretProviderClass and Pod configuration

### RAM Role ARN

RAM Role authentication uses AK/SK as base credentials to assume a RAM Role via STS AssumeRole API. This approach provides temporary credentials instead of long-term AK/SK.

#### Use Cases

- Using AK/SK to assume a RAM Role
- Need temporary credentials instead of long-term AK/SK
- Role-based access control with existing AK/SK

#### Configuration Steps

**Step 1: Create RAM Role**

Create a RAM Role that the RAM User can assume via STS AssumeRole:

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

**Step 2: Grant KMS Permissions to RAM Role**

Create and attach a RAM Policy that grants KMS read permissions to the Role:

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

> **Note**: KMS permissions belong to the **RAM Role**, not the RAM User. The RAM User only needs `sts:AssumeRole` permission.

**Step 3: Grant sts:AssumeRole to RAM User**

Ensure the RAM User has permission to assume the Role:

```bash
# Attach the built-in policy that allows STS AssumeRole
aliyun ram AttachPolicyToUser --PolicyType System --PolicyName AliyunSTSAssumeRoleAccess --UserName <userName>
```

**Step 4: Create Kubernetes Secret**

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

**Step 5: Configure Helm values.yaml**

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

**Step 6: Deploy Provider and Create SecretProviderClass**

No special configuration needed in SecretProviderClass.

#### Prerequisites

1. **RAM User** with AK/SK and `sts:AssumeRole` permission
2. **RAM Role** with:
   - Trust policy: Allow the RAM User to assume
   - Permission policy: KMS access permissions (kms:GetSecretValue, kms:Decrypt)

#### Advantages

- ✅ Temporary credentials (more secure than static AK/SK)
- ✅ Role-based access control

#### Limitations

- ❌ Requires managing AK/SK
- ❌ More complex than RRSA
- ❌ AK/SK still needed as base credential

See the complete example: [ram-role-secretproviderclass.yaml](https://github.com/aliyun/secrets-store-csi-driver-provider-alibaba-cloud/blob/master/examples/ram-role-secretproviderclass.yaml)

### Node Publish Secret

Node Publish Secret authentication stores AK/SK in a Kubernetes Secret and passes it to the Provider via CSI Driver's `nodePublishSecretRef` mechanism.

#### Use Cases

- AK/SK stored in Kubernetes Secret
- Pass credentials via CSI Driver's standard mechanism
- Integration with existing K8s Secret management

#### Configuration Steps

**Step 1: Create Kubernetes Secret**

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

**Step 2: Create SecretProviderClass**

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

**Step 3: Create Pod with nodePublishSecretRef**

> **Important**: When using Node Publish Secret authentication, the Pod's CSI volume **must** explicitly configure `nodePublishSecretRef` to reference the K8s Secret containing AK/SK. This is required by the CSI Driver to pass credentials to the Provider.
>
> **Requirements:**
> - The Secret must exist in the **same namespace** as the Pod
> - The Secret must contain `access_key` and `access_secret` fields

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
      # Required: reference the K8s Secret containing AK/SK
      nodePublishSecretRef:
        name: alibaba-credentials
```

#### Prerequisites

1. **Kubernetes Secret** created with AK/SK
2. **AK/SK** has KMS access permissions
3. **nodePublishSecretRef** configured in Pod CSI volume (same namespace as Pod)

#### Advantages

- ✅ Uses Kubernetes native Secret management
- ✅ Standard CSI Driver mechanism

#### Limitations

- ❌ AK/SK stored in K8s Secret (access control depends on RBAC)
- ❌ Less secure than RRSA
- ❌ AK/SK are long-term credentials without automatic rotation
- ❌ Pod volume must explicitly configure `nodePublishSecretRef`

See the complete example: [node-publish-secretproviderclass.yaml](https://github.com/aliyun/secrets-store-csi-driver-provider-alibaba-cloud/blob/master/examples/node-publish-secretproviderclass.yaml)

### AK/SK

AK/SK static credentials authentication uses long-term AccessKey ID and AccessKey Secret directly. **This method is not recommended for production environments** due to security concerns.

#### Use Cases

- Testing environments
- Quick verification
- Legacy system compatibility

#### ⚠️ Security Warning

- **High risk**: AK/SK can be leaked
- **No isolation**: All Pods share the same permissions
- **Long-term credentials**: Hard to rotate
- **Recommendation**: Use RRSA or RAM Role instead

#### Configuration Steps

**Step 1: Create Kubernetes Secret**

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

**Step 2: Configure Helm values.yaml**

```yaml
envVarsFromSecret:
  ACCESS_KEY_ID:
    secretKeyRef: alibaba-credentials
    key: id
  SECRET_ACCESS_KEY:
    secretKeyRef: alibaba-credentials
    key: secret
```

#### Prerequisites

1. **RAM User** with AK/SK
2. **RAM User** has KMS access permissions

#### Advantages

- ✅ Simple configuration
- ✅ No Role setup required

#### Limitations

- ❌ **Not secure** for production
- ❌ No permission isolation
- ❌ Manual rotation required
- ❌ Does not support temporary credentials

See the complete example: [ak-sk-secretproviderclass.yaml](https://github.com/aliyun/secrets-store-csi-driver-provider-alibaba-cloud/blob/master/examples/ak-sk-secretproviderclass.yaml)

### ECS RAM Role

ECS RAM Role authentication automatically retrieves temporary credentials from the ECS instance metadata service. In ACK clusters, this actually uses the cluster's **Worker RAM Role** (WorkerRole) — the RAM Role attached to the cluster's worker nodes — rather than an arbitrary ECS instance role.

> **Reference**: [Grant permissions to the worker RAM role of an ACK managed cluster](https://www.alibabacloud.com/help/en/ack/product-overview/product-changes-permissions-of-the-worker-ram-role-of-ack-managed-clusters-are-revoked)

#### Use Cases

- Cluster worker nodes already have a Worker RAM Role attached
- Automatic credential rotation
- No AK/SK management required

#### Configuration Steps

Ensure the following:

- The cluster's Worker RAM Role has KMS access permissions (`kms:GetSecretValue`, `kms:Decrypt`)

#### Prerequisites

1. **Worker nodes** with the cluster's Worker RAM Role attached
2. **Worker RAM Role** has KMS access permissions (`kms:GetSecretValue`, `kms:Decrypt`)
3. **No other authentication methods** configured

#### Advantages

- ✅ Automatic credential rotation
- ✅ No AK/SK management

#### Limitations

- ❌ All Pods on same node share permissions
- ❌ Node-level isolation only
- ❌ Requires Worker RAM Role setup

See the complete example: [ecs-ram-role-secretproviderclass.yaml](https://github.com/aliyun/secrets-store-csi-driver-provider-alibaba-cloud/blob/master/examples/ecs-ram-role-secretproviderclass.yaml)

## Advanced Usage

### Cross-Account KMS Access

The provider supports accessing KMS secrets from another Alibaba Cloud account using **all 6 authentication methods**. Cross-account access is a unified capability — regardless of which authentication method you use, you only need to add the `crossAccountRoleArn` parameter to your SecretProviderClass.

- ✅ Pod SA RRSA (Pod-level isolation)
- ✅ Provider RRSA (Cluster-level unified auth)
- ✅ RAM Role (AK/SK + RoleArn)
- ✅ Node Publish Secret (K8s Secret with AK/SK)
- ✅ AK/SK (Static credentials)
- ✅ ECS RAM Role (Automatic credential retrieval)

#### Permission Requirements (Least Privilege)

**Source Account (Cluster Account):**
- ✅ `sts:AssumeRole` permission (to assume target account role)
- ❌ NO KMS permission needed

**Target Account (KMS Account):**
- ✅ Trust policy allowing source account
- ✅ KMS access permission
- ❌ NO STS permission needed

#### Prerequisites

**Target Account (KMS Account):**
1. Create a RAM Role with KMS permissions
2. Configure trust policy to allow Source Account assumption

**Source Account (Cluster Account):**
1. Base credential must have `sts:AssumeRole` permission

#### Setup Steps

**Step 1: Target Account (KMS Account)**

1. Create a RAM Role with KMS permissions
2. Configure trust policy:

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

3. Grant KMS permissions:

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

**Step 2: Source Account (Cluster Account)**

1. Grant `sts:AssumeRole` permission to the base credential (Pod SA role, Provider SA role, Worker RAM Role, or RAM user)

2. Add `crossAccountRoleArn` to each object in SecretProviderClass:

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

#### Example: Pod SA + Cross-Account

Combine Pod SA authentication with cross-account access for maximum security:

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
    
    # Enable Pod SA authentication
    usePodServiceAccountToken: "true"
    
    # Target Account's RAM Role (has KMS permissions)
    # This role MUST trust the source account and have KMS access
    objects: |
      - objectName: "staging/app-secret"
        objectType: "kms"
        objectAlias: "app-secret"
        crossAccountRoleArn: "acs:ram::<TARGET_ACCOUNT_ID>:role/cross-account-kms-role"
```

See the complete example: [cross-account-secretproviderclass.yaml](https://github.com/aliyun/secrets-store-csi-driver-provider-alibaba-cloud/blob/master/examples/cross-account-secretproviderclass.yaml)

#### Troubleshooting

**Error: "base credential is required for cross-account access"**
- Ensure at least one authentication method is configured (RRSA, Worker RAM Role, or AK/SK)

**Error: "failed to assume cross account role"**
- Verify source account has `sts:AssumeRole` permission for the target role
- Check target role's trust policy allows source account
- Verify role ARN format: `acs:ram::<ACCOUNT_ID>:role/<ROLE_NAME>`

**Error: "AccessDenied" when accessing KMS**
- Grant KMS permissions to the target account role (not source account!)
- Check KMS secret exists in target account
- Verify region matches KMS secret location
- Verify KMS secret ARN and region match

#### Security Best Practices

1. **Least Privilege**: Grant minimum required permissions
   - Source account: only `sts:AssumeRole` permission
   - Target account: only required KMS permissions
2. **Separate Roles**: Use different RAM Roles for different applications
3. **Pod SA Isolation**: Prefer Pod SA authentication for pod-level permission isolation

### Resource Cleanup

When a Pod using a SecretProviderClass is deleted, the CSI Driver automatically cleans up the mounted secret files. If `secretObjects` is configured and `syncSecret.enabled: true`, the following cleanup behavior applies:

- **Pod deletion**: The synced K8s Secret is retained as long as the SecretProviderClass exists and other Pods may still reference it
- **SecretProviderClass deletion**: When the SecretProviderClass is deleted and no Pods are using it, the synced K8s Secret is **automatically garbage-collected** by the CSI Driver
- **Namespace deletion**: All associated Secrets and SecretProviderClasses are cleaned up by Kubernetes garbage collection

This ensures no orphaned secrets remain in the cluster after resources are removed.

### JMESPath JSON Parsing

The provider supports extracting specific fields from JSON-formatted secrets using [JMESPath](https://jmespath.org/) expressions. This is useful when a single KMS secret contains multiple key-value pairs and you want to mount each pair as an individual file.

#### Configuration

Use the `jmesPath` field in the SecretProviderClass `objects` to specify which fields to extract:

```yaml
# Suppose a KMS secret "app-config" has JSON content:
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

This will mount three separate files under the mount path: `db-username`, `db-password`, and `db-host`.

#### Notes

- The secret value must be valid JSON; otherwise, the mount will fail
- Each `jmesPath` entry requires both `path` (JMESPath expression) and `objectAlias` (output file name)
- JMESPath supports nested field access, e.g., `path: "credentials.username"` for nested JSON
- If the specified path does not exist in the JSON, the mount will fail with an error

### Secret Rotation

The provider supports automatic secret rotation through the CSI Driver's [RequiresRepublish](https://secrets-store-csi-driver.sigs.k8s.io/topics/secret-auto-rotation.html) mechanism (v1.6.0+). When enabled, kubelet periodically re-invokes `NodePublishVolume` to trigger re-publish, during which the CSI Driver fetches the latest secret content from the provider.

#### Prerequisites

- CSI Driver rotation feature must be enabled via Helm:

  ```bash
  helm upgrade -n <NAMESPACE> csi-secrets-store secrets-store-csi-driver/secrets-store-csi-driver \
    --set enableSecretRotation=true \
    --set rotationPollInterval=1h
  ```

- `requiresRepublish` is a field on the **CSIDriver** object (not SecretProviderClass). It is automatically set by the Helm chart when `enableSecretRotation=true` — no manual configuration is needed.

#### How It Works (v1.6.0+)

1. When the CSIDriver object has `requiresRepublish: true`, kubelet periodically calls `NodePublishVolume` to re-publish the volume
2. During re-publish, the CSI Driver re-requests secret content from the provider
3. The provider fetches the latest secret version from KMS. If the content has changed, the mounted files are updated
4. The Pod receives the updated secret without requiring a restart

> **Note on `rotationPollInterval`**: In v1.6.0, `rotationPollInterval` serves as a **minimum cache interval** (throttle) rather than a precise polling interval. The actual rotation interval is determined by `max(kubelet sync-frequency ≈ 1m, rotationPollInterval)`. For example, if `rotationPollInterval=30s`, the actual interval will be governed by kubelet's sync-frequency (~1m); if `rotationPollInterval=2h`, the actual interval will be approximately 2 hours.

### Kubernetes Secret Sync

The provider supports syncing mounted secrets to native Kubernetes Secrets using the `secretObjects` field in SecretProviderClass. This allows secrets to be consumed as environment variables in addition to files.

#### Prerequisites

- `syncSecret.enabled` must be set to `true` in Helm values (default: `false`)
- This creates the RBAC roles and bindings required for the CSI Driver to sync secret material

#### Configuration

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
        - objectName: db-password   # Must match objectAlias in parameters
          key: password              # Key name in the K8s Secret
```

#### Sync Behavior

- The K8s Secret is created automatically when the first Pod mounts the SecretProviderClass
- The Secret is updated when the mounted secret content changes (e.g., via rotation)
- The Secret is **automatically deleted** when the last Pod using the SecretProviderClass is deleted and the SecretProviderClass itself is removed
- The `secretObjects[].data[].objectName` must match the `objectAlias` (or `objectName` if no alias) in `parameters.objects`

#### Notes

- Without `syncSecret.enabled: true`, `secretObjects` configuration will be silently ignored
- The synced K8s Secret is owned by the CSI Driver; manual modifications will be overwritten
- Multiple SecretProviderClasses can sync to different K8s Secrets

## Troubleshooting

Most errors can be viewed by describing the pod deployment. For the deployment, find the pod names using get pods (use -n **&lt;NAMESPACE&gt;** if you are not using the default namespace):

```shell
kubectl get pods
```

Then describe the pod (substitute the pod ID from above for **&lt;PODID&gt;**, as before use -n if you are not using the default namespace):

```shell
kubectl describe pod/<PODID>
```

Additional information may be available in the provider logs:

```shell
kubectl -n <PROVIDER_NAMESPACE> get pods
kubectl -n <PROVIDER_NAMESPACE> logs pod/<PODID>
```

Where **&lt;PODID&gt;** in this case is the id of the *csi-secrets-store-provider-alibabacloud* pod.

## SecretProviderClass Options

The SecretProviderClass has the following format:

```yaml
apiVersion: secrets-store.csi.x-k8s.io/v1
kind: SecretProviderClass
metadata:
  name: <NAME>
spec:
  provider: alibabacloud   # please using fixed value 'alibabacloud'
  parameters:
```

The parameters section contains the details of the mount request and contain one of the three fields:

* objects: This is a string containing a YAML declaration (described below) of the secrets to be mounted, For example:

  ```yaml
  parameters:
    objects: |
        - objectName: "MySecret"
  ```
* region: An optional field to specify the Alibaba Cloud region to use when retrieving secrets from Secrets Manager. If this field is missing, the provider will lookup the region of the node. This lookup adds overhead to mount requests so clusters using large numbers of pods will benefit from providing the region here.
* pathTranslation: An optional field to specify a substitution character to use when the path separator character (slash on Linux) is used in the file name. If a Secret or parameter name contains the path separator failures will occur when the provider tries to create a mounted file using the name. When not specified the underscore character is used, thus My/Path/Secret will be mounted as My_Path_Secret. This pathTranslation value can either be the string "False" or a single character string. When set to "False", no character substitution is performed.

**Authentication-related parameters:**

* usePodServiceAccountToken: Set to `"true"` to enable Pod SA RRSA authentication. When enabled, the provider will use the Pod's ServiceAccount token for authentication. Default: `"false"`.
* crossAccountRoleArn: (Optional, per-object) RAM Role ARN in the target account for cross-account access. Configured inside each object in `parameters.objects`. Format: `acs:ram::<TARGET_ACCOUNT_ID>:role/<ROLE_NAME>`. When specified, the provider will assume this role after base authentication.

**Syncing secrets to Kubernetes native secrets:**

* To use `secretObjects` in SecretProviderClass to sync secrets to K8s Secrets, you must enable `syncSecret.enabled: true` in Helm values. This creates the RBAC roles and bindings required for the CSI Driver to sync secret material. Without this setting, `secretObjects` configuration will be ignored.

The objects field of the SecretProviderClass can contain the following sub-fields:

* objectName: This field is required. It specifies the name of the secret or parameter to be fetched. For Secrets Manager this is the [SecretName](https://www.alibabacloud.com/help/en/key-management-service/latest/getsecretvalue#parameters) parameter and can be either the friendly name or full ARN of the secret.
* objectType: This optional field specifies the type of secret. Support `kms` and `oos`, defaults to `kms`.
* objectAlias: This optional field specifies the file name under which the secret will be mounted. When not specified the file name defaults to objectName.
* objectVersion: This field is optional, only for KMS secret, and generally not recommended since updates to the secret require updating this field. For Secrets Manager this is the [VersionId](https://www.alibabacloud.com/help/en/key-management-service/latest/getsecretvalue#parameters).
* objectVersionLabel: This optional fields specifies the alias used for the version, only for KMS secret. Most applications should not use this field since the most recent version of the secret is used by default. For Secrets Manager this is the [VersionStage](https://www.alibabacloud.com/help/en/key-management-service/latest/getsecretvalue#parameters).
* jmesPath: This optional field specifies the specific key-value pairs to extract from a JSON-formatted secret. You can use this field to mount key-value pairs from a properly formatted secret value as individual secrets. For example: Consider a secret "test" with JSON content as follows:

  ```shell
  {
    "username": "testuser",
    "password": "testpassword"
  }
  ```

  To mount the username and password key pairs of this secret as individual secrets, use the jmesPath field as follows:

  ```yaml:
  objects: |
      - objectName: "test"
        jmesPath:
            - path: "username"
              objectAlias: "MySecretUsername"
            - path: "password"
              objectAlias: "MySecretPassword"
  ```

  If you use the jmesPath field,  you must provide the following two sub-fields:

  * path: This required field is the [JMES path](https://jmespath.org/specification.html) to use for retrieval
  * objectAlias: This required field specifies the file name under which the key-value pair secret will be mounted.

* kmsEndpoint：The optional field used to specify the endpoint for KMS Secrets Manager. If not specified, the default endpoint is `kms-vpc.{.region-id}.aliyuncs.com`.

  kmsEndpoint configurations introduction：KMS currently supports two access methods: dedicated gateway and shared gateway. The application supports both methods, and different Endpoints need to be configured for each method. Below are the Endpoint address configurations for different gateways:

  | Gateway Type          | Endpoint Address                                 | Usage Instructions                                                                                                                                                                                                                                                |
  | --------------------- | ------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------- |
  | Dedicated Gateway     | {kms-instance-id}.cryptoservice.kms.aliyuncs.com | 1. Requires the KMS instance and the cluster to be in the same Region and VPC.<br />2. Replace {kms-instance-id} with the actual KMS instance ID.<br />3. KMS instance version must be 3.0 or above. |
  | VPC Shared Gateway    | kms-vpc.{region}.aliyuncs.com                    | 1. Requires the KMS instance and the cluster to be in the same Region.<br />2. Replace {region} with the Region where the KMS instance is located.<br />3. This is the default configuration for the application; no additional configuration is needed. |
  | Public Shared Gateway | kms.{region}.aliyuncs.com                        | 1. Replace {region} with the Region where the KMS instance is located.<br />2. The cluster must have public network access capability. |

**Tips**
If there is a special scene that requires the same objectName of the object (As shown in the following example, kms and oos have the same secret name), then you need to set different objectAlias of the object.Otherwise, all the secrets of the previously mounted objects will be overridden by the last one.

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

## Additional Considerations

### Security Considerations

This plugin is built to ensure compatibility between Secret Manager and Kubernetes workloads that need to load secrets from the filesystem. It also enables syncing of those secrets to Kubernetes-native secrets for consumption as environment variables.

When evaluating this plugin consider the following threats:

- When a secret is accessible on the **filesystem**, application vulnerabilities like [directory traversal](https://en.wikipedia.org/wiki/Directory_traversal_attack) attacks can become higher severity as the attacker may gain the ability to read the secret material.
- When a secret is consumed through **environment variables**, misconfigurations such as enabling a debug endpoint or including dependencies that log process environment details may leak secrets.
- When **syncing** secret material to another data store (like Kubernetes Secrets), consider whether the access controls on that data store are sufficiently narrow in scope.

For these reasons, *when possible* we recommend using the Alibaba Cloud Service API directly.

- [Key Management Service API](https://www.alibabacloud.com/help/en/kms/key-management-service/developer-reference/api-getsecretvalue)
- [Encrypt Parameter API](https://www.alibabacloud.com/help/en/oos/developer-reference/api-oos-2019-06-01-getsecretparameter)

## Security

Please report vulnerabilities by email to **kubernetes-security@service.aliyun.com**. Also see our [SECURITY.md](./SECURITY.md) file for details.

## License

This project is licensed under the Apache-2.0 License.
