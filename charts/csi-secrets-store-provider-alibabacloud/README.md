

# Alibaba Cloud Secrets Manager for Secret Store CSI Driver

Alibaba Cloud Secrets Manager provider for Secrets Store CSI driver allows you to get secret contents stored in [Alibaba Cloud Secrets Manager](https://www.alibabacloud.com/help/en/key-management-service/latest/secrets-manager-overview)  and use the Secrets Store CSI driver interface to mount them into Kubernetes pods.

### Prerequisites

- [Helm3](https://helm.sh/docs/intro/quickstart/#install-helm)

### Installing the Chart

- This chart installs the [secrets-store-csi-driver](https://github.com/kubernetes-sigs/secrets-store-csi-driver) and the Alibaba Cloud KMS Secrets Manager provider for the driver

```shell
helm repo add csi-secrets-store-provider-alibabacloud https://raw.githubusercontent.com/AliyunContainerService/secrets-store-csi-driver-provider-alibaba-cloud/main/charts

helm install csi-secrets-store-provider-alibabacloud/csi-secrets-store-provider-alibabacloud --generate-name
```

### Configuration

The following table lists the configurable parameters of the csi-secrets-store-provider-alibabacloud chart and their default values.

> Refer to [doc](https://github.com/kubernetes-sigs/secrets-store-csi-driver/tree/master/charts/secrets-store-csi-driver/README.md) for configurable parameters of the secrets-store-csi-driver chart.

| Parameter                                                    | Description                                                  | Default                                                                                         |
| ------------------------------------------------------------ | ------------------------------------------------------------ |-------------------------------------------------------------------------------------------------|
| `nameOverride`                                               | String to partially override csi-secrets-store-provider-alibabacloud.fullname template with a string (will prepend the release name) | `""`                                                                                            |
| `fullnameOverride`                                           | String to fully override csi-secrets-store-provider-alibabacloud.fullname template with a string | `""`                                                                                            |
| `imagePullSecrets`                                           | Secrets to be used when pulling images                       | `[]`                                                                                            |
| `logFormatJSON`                                              | Use JSON logging format                                      | `false`                                                                                         |
| `logVerbosity`                                               | Log level. Uses V logs (klog)                                | `0`                                                                                             |
| `envVarsFromSecret.ACCESS_KEY_ID`                            | Set the ACCESS_KEY_ID variable to specify the credential RAM AK for building SDK client, which needs to be defined in the secret named **alibaba-credentials** |                                                                                                 |
| `envVarsFromSecret.SECRET_ACCESS_KEY`                        | Set the SECRET_ACCESS_KEY variable to specify the credential RAM SK for building SDK client, which needs to be defined in the secret named **alibaba-credentials** |                                                                                                 |
| `envVarsFromSecret.ALICLOUD_ROLE_ARN`                        | Set the ALICLOUD_ROLE_ARN variable to specify the RAM role ARN for building SDK client, which needs to be defined in the secret named **alibaba-credentials** |                                                                                                 |
| `envVarsFromSecret.ALICLOUD_ROLE_SESSION_NAME`               | Set the ALICLOUD_ROLE_SESSION_NAME variable to specify the RAM role session name for building SDK client, which needs to be defined in the secret named **alibaba-credentials** |                                                                                                 |
| `envVarsFromSecret.ALICLOUD_ROLE_SESSION_EXPIRATION`         | Set the ALICLOUD_ROLE_SESSION_NAME variable to specify the RAM role session expiration for building SDK client, which needs to be defined in the secret named **alibaba-credentials** |                                                                                                 |
| `envVarsFromSecret. ALICLOUD_OIDC_PROVIDER_ARN`              | Set the ALICLOUD_OIDC_PROVIDER_ARN variable to specify the RAM OIDC  provider arn for building SDK client, which needs to be defined in the secret named **alibaba-credentials** |                                                                                                 |
| `envVarsFromSecret.ALICLOUD_OIDC_TOKEN_FILE`                 | Set the ALICLOUD_OIDC_TOKEN_FILE variable to specify the serviceaccount OIDC token file path for building SDK client, which needs to be defined in the secret named **alibaba-credentials** |                                                                                                 |
| rrsa.enable                                                  | Enable RRSA feature, default is false，when enalbe, you need to configure the parametes of  `ALICLOUD_ROLE_ARN` and `ALICLOUD_OIDC_PROVIDER_ARN`  in `envVarsFromSecret` | false                                                                                           |
| `linux.enabled`                                              | Install alibabacloud keyvault provider on linux nodes        | true                                                                                            |
| `linux.image.repository`                                     | Linux image repository                                       | `registry.cn-hangzhou.aliyuncs.com/acs/secrets-store-csi-driver-provider-alibaba-cloud`         |
| `linux.image.pullPolicy`                                     | Linux image pull policy                                      | `Always`                                                                                        |
| `linux.image.tag`                                            | Alibaba Cloud Secrets Manager Provider Linux image tag       | `v1.1.0`                                                                                        |
| `linux.nodeSelector`                                         | Node Selector for the daemonset on linux nodes               | `{}`                                                                                            |
| `linux.tolerations`                                          | Tolerations for the daemonset on linux nodes                 | `{}`                                                                                            |
| `linux.resources`                                            | Resource limit for provider pods on linux nodes              | `requests.cpu: 50m`<br>`requests.memory: 100Mi`<br>`limits.cpu: 100m`<br>`limits.memory: 500Mi` |
| `linux.podLabels`                                            | Additional pod labels                                        | `{}`                                                                                            |
| `linux.podAnnotations`                                       | Additional pod annotations                                   | `{}`                                                                                            |
| `linux.priorityClassName`                                    | Indicates the importance of a Pod relative to other Pods.    | `""`                                                                                            |
| `linux.updateStrategy`                                       | Configure a custom update strategy for the daemonset on linux nodes | `RollingUpdate with 1 maxUnavailable`                                                           |
| `linux.healthzPort`                                          | port for health check                                        | `"8989"`                                                                                        |
| `linux.healthzPath`                                          | path for health check                                        | `"/healthz"`                                                                                    |
| `linux.healthzTimeout`                                       | RPC timeout for health check                                 | `"5s"`                                                                                          |
| `linux.volumes`                                              | Additional volumes to create for the KeyVault provider pods. | `[]`                                                                                            |
| `linux.volumeMounts`                                         | Additional volumes to mount on the KeyVault provider pods.   | `[]`                                                                                            |
| `linux.affinity`                                             | Configures affinity for provider pods on linux nodes         | Match expression `type NotIn virtual-kubelet`                                                   |
| `linux.kubeletRootDir`                                       | Configure the kubelet root dir                               | `/var/lib/kubelet`                                                                              |
| `linux.providersDir`                                         | Configure the providers root dir                             | `/var/run/secrets-store-csi-providers`                                                          |
| `secrets-store-csi-driver.install`                           | Install secrets-store-csi-driver with this chart             | true                                                                                            |
| `secrets-store-csi-driver.fullnameOverride`                  | String to fully override secrets-store-csi-driver.fullname template with a string | `secrets-store-csi-driver`                                                                      |
| `secrets-store-csi-driver.linux.enabled`                     | Install secrets-store-csi-driver on linux nodes              | true                                                                                            |
| `secrets-store-csi-driver.linux.image.repository`            | Driver Linux image repository                                | ` registry.cn-hangzhou.aliyuncs.com/acs/csi-secrets-store-driver`                               |
| `secrets-store-csi-driver.linux.image.pullPolicy`            | Driver Linux image pull policy                               | `Always`                                                                                        |
| `secrets-store-csi-driver.linux.image.tag`                   | Driver Linux image tag                                       | `v1.3.4`                                                                                        |
| `secrets-store-csi-driver.linux.livenessProbeImage.repository` | Linux liveness-probe image repository                        | `registry.cn-hangzhou.aliyuncs.com/acs/csi-secrets-store-livenessprobe`                         |
| `secrets-store-csi-driver.linux.livenessProbeImage.pullPolicy` | Linux liveness-probe image pull policy                       | `Always`                                                                                        |
| `secrets-store-csi-driver.linux.livenessProbeImage.tag`      | Linux liveness-probe image tag                               | `v2.10.0`                                                                                       |
| `secrets-store-csi-driver.linux.registrarImage.repository`   | Linux node-driver-registrar image repository                 | `registry.cn-hangzhou.aliyuncs.com/acs/csi-node-driver-registrar`                               |
| `secrets-store-csi-driver.linux.registrarImage.pullPolicy`   | Linux node-driver-registrar image pull policy                | `Always`                                                                                        |
| `secrets-store-csi-driver.linux.registrarImage.tag`          | Linux node-driver-registrar image tag                        | `v2.8.0`                                                                                        |
| `secrets-store-csi-driver.enableSecretRotation`              | Enable secret rotation feature [alpha]                       | `false`                                                                                         |
| `secrets-store-csi-driver.rotationPollInterval`              | Secret rotation poll interval duration                       | `2m`                                                                                            |
| `secrets-store-csi-driver.filteredWatchSecret`               | Enable filtered watch for NodePublishSecretRef secrets with label `secrets-store.csi.k8s.io/used=true`. Refer to [doc](https://secrets-store-csi-driver.sigs.k8s.io/load-tests.html) for more details | `true`                                                                                          |
| `secrets-store-csi-driver.syncSecret.enabled`                | Enable rbac roles and bindings required for syncing to Kubernetes native secrets | `false`                                                                                         |
| `rbac.install`                                               | Install default service account                              | true                                                                                            |

## Usage

Add your secret data to [Alibaba Cloud Secrets Manager]((https://www.alibabacloud.com/help/en/key-management-service/latest/secrets-manager-overview)) with aliyun CLI tool, firstly use `aliyun configure` to set your credentials and default region info.

Now create a test secret:

```shell
aliyun kms CreateSecret --SecretName test --SecretData 1234 --VersionId v1
```
Create an access policy for the pod scoped down to just the secrets it should have :
```shell
aliyun ram CreatePolicy --PolicyName kms-test --PolicyDocument '{"Statement": [{"Effect": "Allow","Action": "kms:GetSecretValue","Resource": "acs:kms:{region-id}:{aliyun-uid}:secret/test"}],"Version": "1"}'
```

### Enable [RRSA](https://www.alibabacloud.com/help/zh/container-service-for-kubernetes/latest/use-rrsa-to-enforce-access-control#section-ywl-59g-j8h) feature

RAM Roles for Service Accounts (RRSA) is the recommended secure authentication method for obtaining secrets in Alibaba Cloud Secrets Manager. For the configuration, please refer to the following steps:

1. Create the RAM OIDC provider for the cluster with [ack-ram-tool](https://github.com/AliyunContainerService/ack-ram-tool) or reference [RRSA](https://www.alibabacloud.com/help/zh/container-service-for-kubernetes/latest/use-rrsa-to-enforce-access-control#section-ywl-59g-j8h) doc if you have not already done so:

```shell
ack-ram-tool rrsa enable -c <clusterId>
```
2. Next create the service account to be used by the pod and associate the above kms RAM policy with that service account. Here we use [ack-ram-tool](https://github.com/AliyunContainerService/ack-ram-tool) CLI to simplify the steps of RAM role creation and authorization:

```shell
ack-ram-tool rrsa associate-role -c <clusterId> --create-role-if-not-exist -r <roleName> -n <namespace> -s csi-secrets-store-provider-alibabacloud
```

3. Create a secret named `alibaba-credentials` in target cluster, create a template file below named `alibaba-credentials.yaml`:


```yaml
apiVersion: v1
data:
  oidcproviderarn: ****
  rolearn: ****   #specify the assumed ram role ARN, base64 encoding required
kind: Secret
metadata:
  name: alibaba-credentials
  namespace: <namespace>
type: Opaque  
```

**oidcproviderarn**: specify the cluster's OIDC provider ARN, you can obtain the value in [RAM SSO](https://ram.console.aliyun.com/providers) console, then find the target provider ARN in the `OIDC` tab, base64 encoding required
**rolearn**: specify the assumed ram role ARN, base64 encoding required
**namespace **: specify the namespace which will install provider

Run the command to deploy secret:

```bash
kubectl apply -f alibaba-credentials.yaml
```

4. Update below envVarsFromSecret configuration in the values.yaml:

```yaml
envVarsFromSecret:
  ALICLOUD_ROLE_ARN:
    secretKeyRef: alibaba-credentials
    key: rolearn
  ALICLOUD_OIDC_PROVIDER_ARN:
    secretKeyRef: alibaba-credentials
    key: oidcproviderarn

rrsa:
  # Specifies whether using rrsa and enalbe sa token volume projection, default is false
  enable: true
```



Now create the SecretProviderClass which tells the provider which secrets are to be mounted in the pod. The secretproviderclass.yaml in the [examples](./examples) directory will mount "test" created above.

Finally we can deploy our pod. The deploy.yaml in the examples directory contains a sample nginx deployment that mounts the secrets under /mnt/secrets-store in the pod.

To verify the secret has been mounted properly, See the example below:

```shell
kubectl exec -it $(kubectl get pods | awk '/nginx-deployment/{print $1}' | head -1) cat /mnt/secrets-store/test; echo
```
### Troubleshooting
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

### SecretProviderClass options
The SecretProviderClass has the following format:
```yaml
apiVersion: secrets-store.csi.x-k8s.io/v1alpha1
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
* region: An optional field to specify the Alibaba Cloud region to use when retrieving secrets from Secrets Manager or Parameter Store. If this field is missing, the provider will lookup the region from the annotation on the node. This lookup adds overhead to mount requests so clusters using large numbers of pods will benefit from providing the region here.
* pathTranslation: An optional field to specify a substitution character to use when the path separator character (slash on Linux) is used in the file name. If a Secret or parameter name contains the path separator failures will occur when the provider tries to create a mounted file using the name. When not specified the underscore character is used, thus My/Path/Secret will be mounted as My_Path_Secret. This pathTranslation value can either be the string "False" or a single character string. When set to "False", no character substitution is performed.

The objects field of the SecretProviderClass can contain the following sub-fields:
* objectName: This field is required. It specifies the name of the secret or parameter to be fetched. For Secrets Manager this is the [SecretName](https://www.alibabacloud.com/help/en/key-management-service/latest/getsecretvalue#parameters) parameter and can be either the friendly name or full ARN of the secret.

* objectAlias: This optional field specifies the file name under which the secret will be mounted. When not specified the file name defaults to objectName.

* objectVersion: This field is optional, and generally not recommended since updates to the secret require updating this field. For Secrets Manager this is the [VersionId](https://www.alibabacloud.com/help/en/key-management-service/latest/getsecretvalue#parameters).

* objectVersionLabel: This optional fields specifies the alias used for the version. Most applications should not use this field since the most recent version of the secret is used by default. For Secrets Manager this is the [VersionStage](https://www.alibabacloud.com/help/en/key-management-service/latest/getsecretvalue#parameters).

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

## Additional Considerations

### Rotation
When using the optional alpha [rotation reconciler](https://secrets-store-csi-driver.sigs.k8s.io/topics/secret-auto-rotation.html) feature of the Secrets Store CSI driver the driver will periodically remount the secrets in the SecretProviderClass. This will cause additional API calls which results in additional charges. Applications should use a reasonable poll interval that works with their rotation strategy. A one hour poll interval is recommended as a default to reduce excessive API costs.

Anyone wishing to test out the rotation reconciler feature can enable it using helm:
```bash
helm upgrade -n <NAMESPACE> csi-secrets-store secrets-store-csi-driver/secrets-store-csi-driver --set enableSecretRotation=true --set rotationPollInterval=60s
```

### Security Considerations

This plugin is built to ensure compatibility between Secret Manager and Kubernetes workloads that need to load secrets from the filesystem. It also enables syncing of those secrets to Kubernetes-native secrets for consumption as environment variables.

When evaluating this plugin consider the following threats:

- When a secret is accessible on the **filesystem**, application vulnerabilities like [directory traversal](https://en.wikipedia.org/wiki/Directory_traversal_attack) attacks can become higher severity as the attacker may gain the ability to read the secret material.
- When a secret is consumed through **environment variables**, misconfigurations such as enabling a debug endpoint or including dependencies that log process environment details may leak secrets.
- When **syncing** secret material to another data store (like Kubernetes Secrets), consider whether the access controls on that data store are sufficiently narrow in scope.

For these reasons, *when possible* we recommend using the [Secrets Manager API](https://www.alibabacloud.com/help/en/key-management-service/latest/secrets) directly.

## License

This project is licensed under the Apache-2.0 License.