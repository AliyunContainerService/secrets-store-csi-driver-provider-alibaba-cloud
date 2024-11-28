package main

import (
	"flag"
	"fmt"
	t "log"
	"net"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/AliyunContainerService/secrets-store-csi-driver-provider-alibaba-cloud/utils"
	"google.golang.org/grpc/health/grpc_health_v1"

	"golang.org/x/time/rate"
	"google.golang.org/grpc"
	"k8s.io/klog/v2"
	csidriver "sigs.k8s.io/secrets-store-csi-driver/provider/v1alpha1"

	"github.com/AliyunContainerService/secrets-store-csi-driver-provider-alibaba-cloud/auth"
	"github.com/AliyunContainerService/secrets-store-csi-driver-provider-alibaba-cloud/provider"
	"github.com/AliyunContainerService/secrets-store-csi-driver-provider-alibaba-cloud/server"
)

var (
	endpointDir = flag.String("provider-volume", "/etc/kubernetes/secrets-store-csi-providers", "csi gRPC endpoint")

	healthzPort    = flag.Int("healthz-port", 8989, "port for health check")
	healthzPath    = flag.String("healthz-path", "/healthz", "path for health check")
	healthzTimeout = flag.Duration("healthz-timeout", 5*time.Second, "RPC timeout for health check")

	maxConcurrentKmsSecretPulls = flag.Int("max-concurrent-kms-secret-pulls", 10, "used to control how many kms secrets are pulled at the same time.")
	maxConcurrentOosSecretPulls = flag.Int("max-concurrent-oos-secret-pulls", 10, "used to control how many oos secrets are pulled at the same time.")
)

// Main entry point for the Secret Store CSI driver Alibaba Cloud provider. This main
// rountine starts up the gRPC server that will listen for incoming mount
// requests.
func main() {
	klog.Infof("Starting %s version %s", auth.ProviderName, server.Version)
	klog.InitFlags(nil)
	defer klog.Flush()

	flag.Parse() // Parse command line flags

	provider.LimiterInstance.Kms.SecretPullLimiter = rate.NewLimiter(rate.Limit(*maxConcurrentKmsSecretPulls), 1)
	provider.LimiterInstance.OOS.SecretPullLimiter = rate.NewLimiter(rate.Limit(*maxConcurrentOosSecretPulls), 1)

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGTERM, syscall.SIGINT, os.Interrupt)

	//socket on which to listen to for driver calls
	endpoint := fmt.Sprintf("%s/alibabacloud.sock", *endpointDir)
	os.Remove(endpoint) // Make sure to start clean.
	grpcSrv := grpc.NewServer(
		grpc.UnaryInterceptor(utils.LogInterceptor()),
	)

	listener, err := net.Listen("unix", endpoint)
	if err != nil {
		klog.Fatalf("Failed to listen on unix socket. error: %v", err)
	}

	//cfg, err := rest.InClusterConfig()
	//if err != nil {
	//	klog.Fatalf("Can not get cluster config. error: %v", err)
	//}
	//
	//clientset, err := kubernetes.NewForConfig(cfg)
	//if err != nil {
	//	klog.Fatalf("Can not initialize kubernetes client. error: %v", err)
	//}

	defer func() { // Cleanup on shutdown
		listener.Close()
		os.Remove(endpoint)
	}()

	providerSrv, err := server.NewServer()
	if err != nil {
		klog.Fatalf("Could not create server. error: %v", err)
	}
	if providerSrv == nil {
		klog.Fatalf("empty provider server")
	}

	csidriver.RegisterCSIDriverProviderServer(grpcSrv, providerSrv)
	// Register the health service.
	grpc_health_v1.RegisterHealthServer(grpcSrv, providerSrv)

	klog.Infof("Listening for connections on address: %s", listener.Addr())

	go func() {
		if err := grpcSrv.Serve(listener); err != nil {
			t.Fatalf("failed to serve provider server: %v", err)
		}
	}()

	healthz := &server.HealthZ{
		HealthCheckURL: &url.URL{
			Host: net.JoinHostPort("", strconv.FormatUint(uint64(*healthzPort), 10)),
			Path: *healthzPath,
		},
		UnixSocketPath: listener.Addr().String(),
		RPCTimeout:     *healthzTimeout,
	}
	go healthz.Serve()

	<-signalChan
	// gracefully stop the grpc server
	klog.Infof("terminating the server")
	providerSrv.GracefulStop()
}
