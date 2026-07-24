package auth

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func incomingCtx(authHeader string) context.Context {
	md := metadata.New(nil)
	if authHeader != "" {
		md.Set("authorization", authHeader)
	}
	return metadata.NewIncomingContext(context.Background(), md)
}

func TestNewServiceTokenAuthFuncEmptyToken(t *testing.T) {
	if fn := NewServiceTokenAuthFunc(""); fn != nil {
		t.Error("token 为空时应返回 nil（不启用认证）")
	}
}

func TestServiceTokenAuthFunc(t *testing.T) {
	fn := NewServiceTokenAuthFunc("s3cret")
	if fn == nil {
		t.Fatal("token 非空时应返回 AuthFunc")
	}

	cases := []struct {
		name   string
		header string
		want   codes.Code // OK 表示放行
	}{
		{"正确 token", "Bearer s3cret", codes.OK},
		{"错误 token", "Bearer wrong", codes.Unauthenticated},
		{"缺失 authorization 头", "", codes.Unauthenticated},
		{"非 Bearer 方案", "Basic s3cret", codes.Unauthenticated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, err := fn(incomingCtx(tc.header))
			if tc.want == codes.OK {
				if err != nil {
					t.Fatalf("应放行，得到错误: %v", err)
				}
				if ctx == nil {
					t.Error("放行时应返回原 ctx")
				}
				return
			}
			if err == nil {
				t.Fatal("应拒绝，却放行")
			}
			if got := status.Code(err); got != tc.want {
				t.Errorf("code = %v, want %v", got, tc.want)
			}
		})
	}
}
