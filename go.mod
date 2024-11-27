module github.com/AliyunContainerService/secrets-store-csi-driver-provider-alibaba-cloud

go 1.18

require (
	github.com/AliyunContainerService/ack-secret-manager v0.0.0-20220112125214-d31312f5d710
	github.com/alibabacloud-go/darabonba-openapi v0.1.7
	github.com/alibabacloud-go/darabonba-openapi/v2 v2.0.9
	github.com/alibabacloud-go/kms-20160120/v2 v2.0.0
	github.com/alibabacloud-go/oos-20190601/v4 v4.2.2
	github.com/alibabacloud-go/tea v1.2.2
	github.com/aliyun/alibaba-cloud-sdk-go v1.61.1473
	github.com/aliyun/credentials-go v1.3.1
	github.com/jmespath/go-jmespath v0.0.0-20180206201540-c2b33e8439af
	github.com/pkg/errors v0.9.1
	golang.org/x/time v0.0.0-20200630173020-3af7569d3a1e
	google.golang.org/grpc v1.29.1
	k8s.io/klog/v2 v2.8.0
	sigs.k8s.io/secrets-store-csi-driver v0.0.22
	sigs.k8s.io/yaml v1.2.0
)

require (
	github.com/alibabacloud-go/alibabacloud-gateway-spi v0.0.4 // indirect
	github.com/alibabacloud-go/debug v1.0.0 // indirect
	github.com/alibabacloud-go/endpoint-util v1.1.0 // indirect
	github.com/alibabacloud-go/openapi-util v0.1.0 // indirect
	github.com/alibabacloud-go/tea-utils v1.3.9 // indirect
	github.com/alibabacloud-go/tea-utils/v2 v2.0.6 // indirect
	github.com/alibabacloud-go/tea-xml v1.1.3 // indirect
	github.com/clbanning/mxj/v2 v2.5.5 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/go-logr/logr v0.4.0 // indirect
	github.com/gogo/protobuf v1.3.1 // indirect
	github.com/golang/protobuf v1.4.3 // indirect
	github.com/google/gofuzz v1.1.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/tjfoc/gmsm v1.3.2 // indirect
	golang.org/x/crypto v0.18.0 // indirect
	golang.org/x/net v0.20.0 // indirect
	golang.org/x/oauth2 v0.0.0-20200107190931-bf48bf16ab8d // indirect
	golang.org/x/sys v0.16.0 // indirect
	golang.org/x/term v0.16.0 // indirect
	golang.org/x/text v0.14.0 // indirect
	google.golang.org/appengine v1.6.6 // indirect
	google.golang.org/genproto v0.0.0-20201110150050-8816d57aaa9a // indirect
	google.golang.org/protobuf v1.25.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/ini.v1 v1.66.2 // indirect
	gopkg.in/yaml.v2 v2.3.0 // indirect
	k8s.io/apimachinery v0.20.2 // indirect
	k8s.io/client-go v12.0.0+incompatible // indirect
	k8s.io/utils v0.0.0-20210111153108-fddb29f9d009 // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.0.2 // indirect
)

replace k8s.io/client-go => k8s.io/client-go v0.20.2
