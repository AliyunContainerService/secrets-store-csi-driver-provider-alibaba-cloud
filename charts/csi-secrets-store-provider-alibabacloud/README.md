

# csi-secrets-store-provider-alibabacloud

Alibaba Cloud Secret Manager provider for Secrets Store CSI driver allows you to get secret contents stored in [Alibaba Cloud Secrets Manager](https://www.alibabacloud.com/help/en/key-management-service/latest/secrets-manager-overview)  and use the Secrets Store CSI driver interface to mount them into Kubernetes pods.

### Prerequisites

- [Helm3](https://helm.sh/docs/intro/quickstart/#install-helm)

### Installing the Chart

- This chart installs the [secrets-store-csi-driver](https://github.com/kubernetes-sigs/secrets-store-csi-driver) and the Alibaba Cloud KMS Secrets Manager provider for the driver

```shell
helm install csi-secrets-store-provider-alibabacloud/csi-secrets-store-provider-alibabacloud --generate-name
```

### Configuration

The following table lists the configurable parameters of the csi-secrets-store-provider-alibabacloud chart and their default values.

> Refer to [doc](https://github.com/kubernetes-sigs/secrets-store-csi-driver/tree/master/charts/secrets-store-csi-driver/README.md) for configurable parameters of the secrets-store-csi-driver chart.

| Parameter                                                    | Description                                                  | Default                                                      |
| ------------------------------------------------------------ | ------------------------------------------------------------ | ------------------------------------------------------------ |
| `nameOverride`                                               | String to partially override csi-secrets-store-provider-alibabacloud.fullname template with a string (will prepend the release name) | `""`                                                         |
| `fullnameOverride`                                           | String to fully override csi-secrets-store-provider-alibabacloud.fullname template with a string | `""`                                                         |
| `imagePullSecrets`                                           | Secrets to be used when pulling images                       | `[]`                                                         |
| `logFormatJSON`                                              | Use JSON logging format                                      | `false`                                                      |
| `logVerbosity`                                               | Log level. Uses V logs (klog)                                | `0`                                                          |
| `linux.enabled`                                              | Install alibabacloud keyvault provider on linux nodes        | true                                                         |
| `linux.image.repository`                                     | Linux image repository                                       | `mcr.microsoft.com/oss/alibabacloud/secrets-store/provider-alibabacloud` |
| `linux.image.pullPolicy`                                     | Linux image pull policy                                      | `IfNotPresent`                                               |
| `linux.image.tag`                                            | Azure Keyvault Provider Linux image tag                      | `v1.1.0`                                                     |
| `linux.nodeSelector`                                         | Node Selector for the daemonset on linux nodes               | `{}`                                                         |
| `linux.tolerations`                                          | Tolerations for the daemonset on linux nodes                 | `{}`                                                         |
| `linux.resources`                                            | Resource limit for provider pods on linux nodes              | `requests.cpu: 50m`<br>`requests.memory: 100Mi`<br>`limits.cpu: 50m`<br>`limits.memory: 100Mi` |
| `linux.podLabels`                                            | Additional pod labels                                        | `{}`                                                         |
| `linux.podAnnotations`                                       | Additional pod annotations                                   | `{}`                                                         |
| `linux.priorityClassName`                                    | Indicates the importance of a Pod relative to other Pods.    | `""`                                                         |
| `linux.updateStrategy`                                       | Configure a custom update strategy for the daemonset on linux nodes | `RollingUpdate with 1 maxUnavailable`                        |
| `linux.healthzPort`                                          | port for health check                                        | `"8989"`                                                     |
| `linux.healthzPath`                                          | path for health check                                        | `"/healthz"`                                                 |
| `linux.healthzTimeout`                                       | RPC timeout for health check                                 | `"5s"`                                                       |
| `linux.volumes`                                              | Additional volumes to create for the KeyVault provider pods. | `[]`                                                         |
| `linux.volumeMounts`                                         | Additional volumes to mount on the KeyVault provider pods.   | `[]`                                                         |
| `linux.affinity`                                             | Configures affinity for provider pods on linux nodes         | Match expression `type NotIn virtual-kubelet`                |
| `linux.kubeletRootDir`                                       | Configure the kubelet root dir                               | `/var/lib/kubelet`                                           |
| `linux.providersDir`                                         | Configure the providers root dir                             | `/var/run/secrets-store-csi-providers`                       |
| `secrets-store-csi-driver.install`                           | Install secrets-store-csi-driver with this chart             | true                                                         |
| `secrets-store-csi-driver.fullnameOverride`                  | String to fully override secrets-store-csi-driver.fullname template with a string | `secrets-store-csi-driver`                                   |
| `secrets-store-csi-driver.linux.enabled`                     | Install secrets-store-csi-driver on linux nodes              | true                                                         |
| `secrets-store-csi-driver.linux.kubeletRootDir`              | Configure the kubelet root dir                               | `/var/lib/kubelet`                                           |
| `secrets-store-csi-driver.linux.metricsAddr`                 | The address the metric endpoint binds to                     | `:8080`                                                      |
| `secrets-store-csi-driver.linux.image.repository`            | Driver Linux image repository                                | `mcr.microsoft.com/oss/kubernetes-csi/secrets-store/driver`  |
| `secrets-store-csi-driver.linux.image.pullPolicy`            | Driver Linux image pull policy                               | `IfNotPresent`                                               |
| `secrets-store-csi-driver.linux.image.tag`                   | Driver Linux image tag                                       | `v1.1.2`                                                     |
| `secrets-store-csi-driver.rotationPollInterval`              | Secret rotation poll interval duration                       | `2m`                                                         |
| `secrets-store-csi-driver.filteredWatchSecret`               | Enable filtered watch for NodePublishSecretRef secrets with label `secrets-store.csi.k8s.io/used=true`. Refer to [doc](https://secrets-store-csi-driver.sigs.k8s.io/load-tests.html) for more details | `true`                                                       |
| `secrets-store-csi-driver.syncSecret.enabled`                | Enable rbac roles and bindings required for syncing to Kubernetes native secrets | `false`                                                      |
| `rbac.install`                                               | Install default service account                              | true                                                         |

