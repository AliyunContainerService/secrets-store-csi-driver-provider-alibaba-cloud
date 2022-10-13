package utils

import (
	"context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"
	"time"
)

// LogInterceptor returns a new unary server interceptors that performs request
// and response logging.
func LogInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()
		deadline, _ := ctx.Deadline()
		dd := time.Until(deadline).String()
		if klog.V(3).Enabled() {
			klog.V(3).InfoS("request", "method", info.FullMethod, "deadline", dd)
		}
		resp, err := handler(ctx, req)
		if klog.V(2).Enabled() {
			s, _ := status.FromError(err)
			klog.V(2).InfoS("response", "method", info.FullMethod, "deadline", dd, "duration", time.Since(start).String(), "status.code", s.Code(), "status.message", s.Message())
		}
		return resp, err
	}
}
