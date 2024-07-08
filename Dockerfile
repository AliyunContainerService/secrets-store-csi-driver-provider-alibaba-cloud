FROM golang:1.22.5 as builder
ENV GO111MODULE off
WORKDIR /go/src/github.com/AliyunContainerService/secrets-store-csi-driver-provider-alibaba-cloud
COPY . .
RUN make build

FROM alpine:3.11.6
WORKDIR /bin

RUN apk update && apk upgrade
RUN apk add --no-cache ca-certificates && \
    update-ca-certificates

COPY --from=builder /go/src/github.com/AliyunContainerService/secrets-store-csi-driver-provider-alibaba-cloud/build/bin/secrets-store-csi-driver-provider-alibaba-cloud /bin/secrets-store-csi-driver-provider-alibaba-cloud

ENTRYPOINT ["secrets-store-csi-driver-provider-alibaba-cloud"]
