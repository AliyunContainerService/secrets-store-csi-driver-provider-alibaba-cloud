module github.com/AliyunContainerService/secrets-store-csi-driver-provider-alibaba-cloud

go 1.16

require (
	github.com/AliyunContainerService/ack-secret-manager v0.0.0-20220112125214-d31312f5d710
	github.com/alibabacloud-go/darabonba-openapi v0.1.7
	github.com/alibabacloud-go/kms-20160120/v2 v2.0.0
	github.com/alibabacloud-go/sts-20150401 v1.1.0 // indirect
	github.com/alibabacloud-go/tea v1.1.15
	github.com/aliyun/alibaba-cloud-sdk-go v1.61.1473
	github.com/aliyun/credentials-go v1.2.2
	github.com/jmespath/go-jmespath v0.0.0-20180206201540-c2b33e8439af
	github.com/pkg/errors v0.9.1
	google.golang.org/grpc v1.29.1
	k8s.io/api v0.20.2 // indirect
	k8s.io/apimachinery v0.20.2 // indirect
	k8s.io/client-go v12.0.0+incompatible // indirect
	k8s.io/klog/v2 v2.8.0
	sigs.k8s.io/secrets-store-csi-driver v0.0.22
	sigs.k8s.io/yaml v1.2.0
)

replace k8s.io/client-go => k8s.io/client-go v0.20.2
