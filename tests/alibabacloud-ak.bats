#!/usr/bin/env bats

load helpers

WAIT_TIME=120
SLEEP_TIME=1
NAMESPACE=kube-system
POD_NAME=basic-test-mount

setup() {
  if [[ -z "${ALIBABA_ACCESS_KEY}" ]] || [[ -z "${ALIBABA_ACCESS_SECRET}" ]]; then
    echo "Error: ram ak/sk is not provided" >&2
    return 1
  fi
}

setup_file() {
    #Create test secrets
    aliyun kms CreateSecret --SecretName testBasic --SecretData testValue --VersionId v1
    aliyun kms CreateSecret --SecretName testSync --SecretData  syncValue --VersionId v1
    aliyun kms CreateSecret --SecretName testRotation --SecretData beforeRotation --VersionId v1

    aliyun kms CreateSecret --SecretName testJson --SecretData '{"username": "testUser", "password": "testPassword"}' --VersionId v1
}

teardown_file() {
    aliyun kms DeleteSecret --SecretName testBasic --ForceDeleteWithoutRecovery true
    aliyun kms DeleteSecret --SecretName testSync --ForceDeleteWithoutRecovery true
    aliyun kms DeleteSecret --SecretName testRotation --ForceDeleteWithoutRecovery true

    aliyun kms DeleteSecret --SecretName testJson --ForceDeleteWithoutRecovery true
}

validate_jsme_mount() {
    result=$(kubectl --namespace $NAMESPACE exec $POD_NAME -- cat /mnt/secrets-store/$USERNAME_ALIAS)
    [[ "${result//$'\r'}" == $USERNAME ]]

    result=$(kubectl --namespace $NAMESPACE exec $POD_NAME -- cat /mnt/secrets-store/$PASSWORD_ALIAS)
    [[ "${result//$'\r'}" == $PASSWORD ]]

    result=$(kubectl --namespace $NAMESPACE exec $POD_NAME -- cat /mnt/secrets-store/$SECRET_FILE_NAME)
    [[ "${result//$'\r'}" == $SECRET_FILE_CONTENT ]]

    run kubectl get secret --namespace $NAMESPACE $K8_SECRET_NAME
    [ "$status" -eq 0 ]

    result=$(kubectl --namespace=$NAMESPACE get secret $K8_SECRET_NAME -o jsonpath="{.data.username}" | base64 -d)
    [[ "$result" == $USERNAME ]]

    result=$(kubectl --namespace=$NAMESPACE get secret $K8_SECRET_NAME -o jsonpath="{.data.password}" | base64 -d)
    [[ "$result" == $PASSWORD ]]
}

@test "Install alibabacloud provider" {
    ali_uid=$(aliyun cs GET /clusters/"${CLUSTER_ID}" |jq '.parameters."ALIYUN::AccountId"'| tr -d '"')
    export ali_uid

    export roleArn=$(echo acs:ram::"${ali_uid}":role/csi-secret-driver-provider-rrsa | tr -d \\n |base64)
    export oidcProviderArn=$(echo acs:ram::"${ali_uid}":oidc-provider/ack-rrsa-"${CLUSTER_ID}" | tr -d \\n |base64)

    #export roleArn=$(base64 <<< "acs:ram::"${ali_uid}":role/csi-secret-driver-provider-rrsa")
    #export oidcProviderArn=$(base64 <<< "acs:ram::"${ali_uid}":oidc-provider/ack-rrsa-"${CLUSTER_ID}"")
    envsubst < alibaba-credentials.yaml | kubectl --namespace $NAMESPACE apply -f -

    #Install csi secret driver
    helm repo add csi-secrets-store-provider-alibabacloud https://raw.githubusercontent.com/AliyunContainerService/secrets-store-csi-driver-provider-alibaba-cloud/main/charts
    helm install -f values.yaml csi-secrets-store-provider-alibabacloud/csi-secrets-store-provider-alibabacloud --namespace kube-system --generate-name --set secrets-store-csi-driver.enableSecretRotation=true --set secrets-store-csi-driver.rotationPollInterval=10s --set secrets-store-csi-driver.syncSecret.enabled=true

    cmd="kubectl --namespace $NAMESPACE wait --for=condition=Ready --timeout=60s pod -l app=csi-secrets-store-provider-alibabacloud"
    wait_for_process $WAIT_TIME $SLEEP_TIME "$cmd"

    PROVIDER_POD=$(kubectl --namespace $NAMESPACE get pod -l app=csi-secrets-store-provider-alibabacloud -o jsonpath="{.items[0].metadata.name}")
    run kubectl --namespace $NAMESPACE get pod/$PROVIDER_POD
    assert_success
}

@test "secretproviderclasses crd is established" {
    cmd="kubectl wait --namespace $NAMESPACE --for condition=established --timeout=60s crd/secretproviderclasses.secrets-store.csi.x-k8s.io"
    wait_for_process $WAIT_TIME $SLEEP_TIME "$cmd"

    run kubectl get crd/secretproviderclasses.secrets-store.csi.x-k8s.io
    assert_success
}

@test "create alibabacloud k8s secret" {
  run kubectl create secret generic secrets-store-creds --from-literal access_key=${ALIBABA_ACCESS_KEY} --from-literal access_secret=${ALIBABA_ACCESS_SECRET} --namespace=$NAMESPACE
  assert_success

  # label the node publish secret ref secret
  run kubectl label secret secrets-store-creds secrets-store.csi.k8s.io/used=true --namespace=$NAMESPACE
  assert_success
}


@test "deploy alibabacloud secretproviderclass crd" {
    envsubst < BasicTestMountSPC.yaml | kubectl apply -f -

    cmd="kubectl --namespace $NAMESPACE get secretproviderclasses.secrets-store.csi.x-k8s.io/basic-test-mount-spc -o yaml | grep alibabacloud"
    wait_for_process $WAIT_TIME $SLEEP_TIME "$cmd"
}

@test "CSI inline volume test with pod portability" {
   kubectl --namespace $NAMESPACE  apply -f BasicTestMountWithSecret.yaml
   cmd="kubectl --namespace $NAMESPACE  wait --for=condition=Ready --timeout=60s pod/basic-test-mount"
   wait_for_process $WAIT_TIME $SLEEP_TIME "$cmd"

   run kubectl --namespace $NAMESPACE  get pod/$POD_NAME
   assert_success
}

@test "CSI inline volume test with rotation" {
   result=$(kubectl --namespace $NAMESPACE exec $POD_NAME -- cat /mnt/secrets-store/testRotation)
   [[ "${result//$'\r'}" == "beforeRotation" ]]

   aliyun kms PutSecretValue --SecretName testRotation --SecretData afterRotation --VersionId v2
   sleep 20
   result=$(kubectl --namespace $NAMESPACE exec $POD_NAME -- cat /mnt/secrets-store/testRotation)
   [[ "${result//$'\r'}" == "afterRotation" ]]
}

@test "CSI inline volume test with pod portability - read secrets manager secrets from pod" {
    result=$(kubectl --namespace $NAMESPACE exec $POD_NAME -- cat /mnt/secrets-store/testBasic)
    [[ "${result//$'\r'}" == "testValue" ]]
}

@test "CSI inline volume test with pod portability - specify jsmePath for Secrets Manager secret with rotation" {

    JSON_CONTENT='{"username": "testUser", "password": "testPassword"}'

    USERNAME_ALIAS=mySecretUsername USERNAME=testUser PASSWORD_ALIAS=mySecretPassword \
    PASSWORD=testPassword SECRET_FILE_NAME=testJson SECRET_FILE_CONTENT=$JSON_CONTENT
    K8_SECRET_NAME=sm-secret-json validate_jsme_mount

    UPDATED_JSON_CONTENT='{"username": "testUser", "password": "testPasswordUpdated"}'
    aliyun kms PutSecretValue --SecretName testJson --SecretData "$UPDATED_JSON_CONTENT" --VersionId v2

    sleep 20
    USERNAME_ALIAS=mySecretUsername USERNAME=testUser PASSWORD_ALIAS=mySecretPassword \
    PASSWORD=testPasswordUpdated SECRET_FILE_NAME=testJson SECRET_FILE_CONTENT=$UPDATED_JSON_CONTENT
    K8_SECRET_NAME=sm-secret-json validate_jsme_mount
}

@test "Sync with Kubernetes Secret" {
    run kubectl get secret --namespace $NAMESPACE  sm-secret
    [ "$status" -eq 0 ]

    result=$(kubectl --namespace=$NAMESPACE get secret sm-secret -o jsonpath="{.data.username}" | base64 -d)
    [[ "$result" == "syncValue" ]]
}

@test "Sync with Kubernetes Secret - Delete deployment. Secret should also be deleted" {
    run kubectl --namespace $NAMESPACE  delete -f BasicTestMount.yaml
    assert_success

    run wait_for_process $WAIT_TIME $SLEEP_TIME "check_secret_deleted secret $NAMESPACE"
    assert_success
}
