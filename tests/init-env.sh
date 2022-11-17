#!/usr/bin/env bash
set -e

usage() {
	echo "usage: $0 --cluster-id <cluster-id>"
}

install_env() {
    if [ -z ${cluster_id} ]; then
      echo "invaild argument"
    fi

    aliyun configure
    temp_dir=$(mktemp -d)
    export temp_dir
    aliyun cs GET /k8s/"${cluster_id}"/user_config | jq -r .config > "${temp_dir}"/kubeconfig.yaml
    export KUBECONFIG=${temp_dir}/kubeconfig.yaml

    ali_uid=$(aliyun cs GET /clusters/"${cluster_id}" |jq '.parameters."ALIYUN::AccountId"'| tr -d '"')
    export ali_uid
    set +e
    aliyun ram CreatePolicy --PolicyName kms-test --PolicyDocument '{"Statement": [{"Effect": "Allow","Action": "kms:GetSecretValue","Resource": "acs:kms:{region-id}:{aliyun-uid}:secret/test*"}],"Version": "1"}'
    set -e

    #rrsa config
    ack-ram-tool rrsa enable -c $cluster_id --assume-yes
    ack-ram-tool rrsa associate-role -c $cluster_id --create-role-if-not-exist -r csi-secret-driver-provider-rrsa -n kube-system -s csi-secrets-store-provider-alibabacloud --assume-yes
}


while [[ $# -ge 1 ]]
do
    key=$1
    shift
    case "$key" in
        --cluster-id)
            export cluster_id=$1
            shift
            ;;
        *)
            usage
            exit 1
            ;;
    esac
done

install_env
