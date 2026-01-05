package rpc

import (
	"context"
	"log/slog"
	"net"
	"runtime/debug"
	"time"

	"go.uber.org/fx"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"
)

func NewServer(cfg Config, l *slog.Logger) *grpc.Server {
	kaParams := grpc.KeepaliveParams(keepalive.ServerParameters{
		MaxConnectionIdle:     cfg.MaxConnectionIdle,
		Time:                  60 * time.Second,
		Timeout:               20 * time.Second,
	})

	opts := []grpc.ServerOption{
		kaParams,
		grpc.ChainUnaryInterceptor(RecoverInterceptor(l)),
	}

	return grpc.NewServer(opts...)
}

func RecoverInterceptor(l *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				l.ErrorContext(ctx, "grpc_panic", slog.Any("panic", r), slog.String("stack", string(debug.Stack())))
				err = status.Errorf(codes.Internal, "internal server error")
			}
		}()
		return handler(ctx, req)
	}
}

func StartLifecycle(lc fx.Lifecycle, s *grpc.Server, cfg Config, l *slog.Logger) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			go func() {
				lis, err := net.Listen("tcp", cfg.Port)
				if err != nil {
					l.Error("grpc_listen_failed", slog.Any("err", err))
					return
				}
				l.Info("grpc_server_start", slog.String("addr", cfg.Port))
				if err := s.Serve(lis); err != nil {
					l.Error("grpc_serve_failed", slog.Any("err", err))
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			l.Info("grpc_server_stop")
			s.GracefulStop()
			return nil
		},
	})
}
