package middleware

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"goKit/internal/application/oplog"
	domain "goKit/internal/domain/oplog"
	"goKit/pkg/kit/auth"
)

// fakeOplogRepo 捕获写入的日志并通过 channel 通知（中间件异步落库）
type fakeOplogRepo struct {
	recorded chan *domain.OperationLog
}

func newFakeOplogRepo() *fakeOplogRepo {
	return &fakeOplogRepo{recorded: make(chan *domain.OperationLog, 8)}
}

func (f *fakeOplogRepo) Create(_ context.Context, l *domain.OperationLog) error {
	f.recorded <- l
	return nil
}

func (f *fakeOplogRepo) List(_ context.Context, _ domain.Query, _, _ int) ([]domain.OperationLog, int64, error) {
	return nil, 0, nil
}

func waitRecord(t *testing.T, f *fakeOplogRepo) *domain.OperationLog {
	t.Helper()
	select {
	case l := <-f.recorded:
		return l
	case <-time.After(2 * time.Second):
		t.Fatal("操作日志未在超时内落库")
		return nil
	}
}

func newOplogTestApp(f *fakeOplogRepo) *fiber.App {
	app := fiber.New()
	svc := oplog.NewOplogService(f)
	l := slog.New(slog.NewTextHandler(io.Discard, nil))
	app.Use(func(c *fiber.Ctx) error { // 模拟 JWTAuth：注入当前用户
		if c.Get("X-Test-User") == "1" {
			c.SetUserContext(auth.WithCurrentUser(c.UserContext(), &auth.CurrentUser{UserID: 7, Username: "admin"}))
		}
		return c.Next()
	})
	app.Use(OperationLog(svc, l))
	app.Post("/users", func(c *fiber.Ctx) error { return c.SendStatus(201) })
	app.Get("/users", func(c *fiber.Ctx) error { return c.SendStatus(200) })
	app.Post("/auth/login", func(c *fiber.Ctx) error { return c.SendStatus(200) })
	return app
}

func TestOperationLogRecordsWriteRequest(t *testing.T) {
	f := newFakeOplogRepo()
	app := newOplogTestApp(f)

	req, _ := httpRequest("POST", "/users", "")
	req.Header.Set("X-Test-User", "1")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 201 {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}

	entry := waitRecord(t, f)
	if entry.UserID != 7 || entry.Username != "admin" {
		t.Errorf("操作人 = %d/%s, want 7/admin", entry.UserID, entry.Username)
	}
	if entry.Method != "POST" || entry.Path != "/users" {
		t.Errorf("method/path = %s %s", entry.Method, entry.Path)
	}
	if entry.Status != 201 {
		t.Errorf("status = %d, want 201", entry.Status)
	}
}

func TestOperationLogSkipsGet(t *testing.T) {
	f := newFakeOplogRepo()
	app := newOplogTestApp(f)

	req, _ := httpRequest("GET", "/users", "")
	if _, err := app.Test(req); err != nil {
		t.Fatal(err)
	}
	select {
	case l := <-f.recorded:
		t.Fatalf("GET 请求不应记录，却写入了 %+v", l)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestOperationLogLoginUsesBodyUsername(t *testing.T) {
	f := newFakeOplogRepo()
	app := newOplogTestApp(f)

	req, _ := httpRequest("POST", "/auth/login", `{"username":"bob","password":"x"}`)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	entry := waitRecord(t, f)
	if entry.Username != "bob" {
		t.Errorf("username = %q, want bob（来自请求体）", entry.Username)
	}
	if entry.UserID != 0 {
		t.Errorf("user_id = %d, want 0（未登录）", entry.UserID)
	}
}

func TestLoginRateLimiter(t *testing.T) {
	app := fiber.New()
	app.Use(ErrorHandler(slog.New(slog.NewTextHandler(io.Discard, nil))))
	app.Post("/auth/login", LoginRateLimiter(2, time.Minute), func(c *fiber.Ctx) error {
		return c.SendStatus(200)
	})

	for i := 1; i <= 3; i++ {
		req, _ := httpRequest("POST", "/auth/login", `{"username":"a","password":"bbbbbbbb"}`)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		if i <= 2 && resp.StatusCode != 200 {
			t.Fatalf("第 %d 次请求 status = %d, want 200", i, resp.StatusCode)
		}
		if i == 3 && resp.StatusCode != 429 {
			t.Fatalf("第 3 次请求 status = %d, want 429", resp.StatusCode)
		}
	}
}

// httpRequest 构造测试请求（避免引入 httptest 噪音的小工具）
func httpRequest(method, path, body string) (*http.Request, error) {
	var r *http.Request
	var err error
	if body == "" {
		r, err = http.NewRequest(method, path, nil)
	} else {
		r, err = http.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	return r, err
}
