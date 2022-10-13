DOCKER_REGISTRY ?= "registry.cn-hangzhou.aliyuncs.com/acs"
BINARY_NAME=secrets-store-csi-driver-provider-alibaba-cloud
PROVIDER_VERSION=v0.1.0
GO111MODULE=on
# Image URL to use all building/pushing image targets
IMG = ${DOCKER_REGISTRY}/${BINARY_NAME}:${PROVIDER_VERSION}

BUILD_FLAGS=-ldflags "-X main.version=${PROVIDER_VERSION}"

MAJOR_REV=1
MINOR_REV=0
$(eval PATCH_REV=$(shell git describe --always))
$(eval BUILD_DATE=$(shell date -u +%Y.%m.%d.%H.%M))
FULL_REV=$(MAJOR_REV).$(MINOR_REV).$(PATCH_REV)-$(BUILD_DATE)

LDFLAGS?="-X github.com/AliyunContainerService/secrets-store-csi-driver-provider-alibaba-cloud/server.Version=$(FULL_REV) -extldflags "-static""

build: *.go fmt
	CGO_ENABLED=0  go build -ldflags $(LDFLAGS) -o build/bin/$(BINARY_NAME) github.com/AliyunContainerService/$(BINARY_NAME)
build-race: *.go fmt
	CGO_ENABLED=0  go build -ldflags $(LDFLAGS) -race -o build/bin/$(BINARY_NAME) github.com/AliyunContainerService/$(BINARY_NAME)

build-image:
	CGO_ENABLED=0  go build -ldflags $(LDFLAGS) -o build/bin/$(BINARY_NAME) github.com/AliyunContainerService/$(BINARY_NAME)
	docker build --build-arg PROVIDER_VERSION=${PROVIDER_VERSION} -t ${IMG} .

# Run tests
test: generate fmt vet manifests
	go test -v ./backend... ./errors/... ./controllers/... -coverprofile cover.out

# Build manager binary
manager: generate fmt vet
	go build ${BUILD_FLAGS} -o bin/${BINARY_NAME} main.go

# Run against the configured Kubernetes cluster in ~/.kube/config
run: generate fmt vet
	go run ./main.go

# Run go fmt against code
fmt:
	go fmt ./...

# Run go vet against code
vet:
	go vet ./...

# Build the docker image
docker-build: test
	docker build --build-arg PROVIDER_VERSION=${PROVIDER_VERSION} -t ${IMG} .

# Push the docker image
docker-push:
	docker push ${IMG}
