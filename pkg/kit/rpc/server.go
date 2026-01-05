package rpc

import (
	"context"
	"log/slog"
	"net"
	"runtime/debug"
	"time"

	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/auth"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/validator"
	"go.uber.org/fx"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"
)

// ServerParams 注入参数
type ServerParams struct {
	fx.In

	Config Config
	Logger *slog.Logger

	// 【新增】AuthFunc 认证函数 (可选注入)
	// 如果没有 Provide 这个函数，Fx 会将其置为 nil
	AuthFunc auth.AuthFunc `optional:"true"`

	// Unary 和 Stream 自定义拦截器插槽
	UnaryInterceptors  []grpc.UnaryServerInterceptor  `group:"grpc_unary_interceptor"`
	StreamInterceptors []grpc.StreamServerInterceptor `group:"grpc_stream_interceptor"`
}

func NewServer(params ServerParams) *grpc.Server {
	// 1. KeepAlive 参数配置
	kaParams := grpc.KeepaliveParams(keepalive.ServerParameters{
		MaxConnectionIdle: params.Config.MaxConnectionIdle,
		Time:              60 * time.Second,
		Timeout:           20 * time.Second,
	})

	// ---------------------------------------------------------
	// 2. 组装 Unary (一元) 拦截器链
	// 顺序: Recovery -> Validator -> Auth -> Custom
	// ---------------------------------------------------------
	unaryChain := []grpc.UnaryServerInterceptor{
		// 1. Panic 恢复 (最外层，兜底)
		RecoverInterceptor(params.Logger),
		// 2. 参数校验 (依赖 proto 生成的 Validate 方法)
		validator.UnaryServerInterceptor(),
	}

	// 3. 认证 (如果有注入 AuthFunc)
	if params.AuthFunc != nil {
		unaryChain = append(unaryChain, auth.UnaryServerInterceptor(params.AuthFunc))
	}

	// 4. 自定义/业务拦截器 (Logging, Tracing, Metrics 等)
	unaryChain = append(unaryChain, params.UnaryInterceptors...)

	// ---------------------------------------------------------
	// 3. 组装 Stream (流式) 拦截器链
	// 保持与 Unary 相同的逻辑顺序
	// ---------------------------------------------------------
	streamChain := []grpc.StreamServerInterceptor{
		RecoverStreamInterceptor(params.Logger),
		validator.StreamServerInterceptor(),
	}

	if params.AuthFunc != nil {
		streamChain = append(streamChain, auth.StreamServerInterceptor(params.AuthFunc))
	}

	streamChain = append(streamChain, params.StreamInterceptors...)

	// ---------------------------------------------------------
	// 4. 创建 Server
	// ---------------------------------------------------------
	opts := []grpc.ServerOption{
		kaParams,
		grpc.ChainUnaryInterceptor(unaryChain...),
		grpc.ChainStreamInterceptor(streamChain...),
	}

	return grpc.NewServer(opts...)
}

// RecoverInterceptor 一元请求 Panic 恢复
func RecoverInterceptor(l *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				l.ErrorContext(ctx, "grpc_panic",
					slog.Any("panic", r),
					slog.String("stack", string(debug.Stack())),
					slog.String("method", info.FullMethod),
				)
				err = status.Errorf(codes.Internal, "internal server error")
			}
		}()
		return handler(ctx, req)
	}
}

// RecoverStreamInterceptor 流式请求 Panic 恢复
func RecoverStreamInterceptor(l *slog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		defer func() {
			if r := recover(); r != nil {
				l.Error("grpc_stream_panic",
					slog.Any("panic", r),
					slog.String("method", info.FullMethod),
				)
			}
		}()
		return handler(srv, ss)
	}
}

// StartLifecycle 生命周期管理
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

// AsUnaryInterceptor 注册一元拦截器
func AsUnaryInterceptor(f any) any {
	return fx.Annotate(f, fx.ResultTags(`group:"grpc_unary_interceptor"`))
}

// AsStreamInterceptor 注册流式拦截器
func AsStreamInterceptor(f any) any {
	return fx.Annotate(f, fx.ResultTags(`group:"grpc_stream_interceptor"`))
}
