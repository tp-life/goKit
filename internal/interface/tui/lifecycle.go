package tui

import (
	"context"
	"log/slog"
	"sync"

	service "goKit/internal/application/service"

	tea "github.com/charmbracelet/bubbletea"
	"go.uber.org/fx"
)

// Program 负责把 Polymarket 服务状态投影到终端界面。
type Program struct {
	cfg        Config
	svc        *service.PolymarketService
	shutdowner fx.Shutdowner
	logger     *slog.Logger

	mu      sync.Mutex
	program *tea.Program
	done    chan struct{}
}

// NewProgram 创建 TUI 运行器；实际是否启动由生命周期阶段决定。
func NewProgram(cfg Config, svc *service.PolymarketService, shutdowner fx.Shutdowner, logger *slog.Logger) *Program {
	return &Program{
		cfg:        cfg,
		svc:        svc,
		shutdowner: shutdowner,
		logger:     logger,
	}
}

// RegisterLifecycle 把 TUI 界面接到 Fx 生命周期中。
func RegisterLifecycle(lc fx.Lifecycle, program *Program) {
	lc.Append(fx.Hook{
		OnStart: program.Start,
		OnStop:  program.Stop,
	})
}

// Start 在需要时启动 Bubble Tea 界面，并复用服务层的订阅流。
func (p *Program) Start(_ context.Context) error {
	if !p.cfg.TUIEnabled() {
		return nil
	}

	// 订阅 dashboard 快照，保证 TUI 与 Web 面板看到的是同一份状态。
	subID, updates := p.svc.Subscribe()
	model := newModel(p.cfg, p.svc, updates)
	program := tea.NewProgram(model, tea.WithAltScreen())

	p.mu.Lock()
	p.program = program
	p.done = make(chan struct{})
	done := p.done
	p.mu.Unlock()

	go func() {
		defer close(done)
		defer p.svc.Unsubscribe(subID)

		if _, err := program.Run(); err != nil && p.logger != nil {
			p.logger.Error("polymarket_tui_exit", slog.Any("err", err))
		}

		// 当用户主动退出 TUI 时，同时把整个应用优雅停掉，避免后台继续挂着。
		_ = p.shutdowner.Shutdown()
	}()

	return nil
}

// Stop 在应用退出时关闭 TUI，并等待界面协程收尾。
func (p *Program) Stop(ctx context.Context) error {
	if !p.cfg.TUIEnabled() {
		return nil
	}

	p.mu.Lock()
	program := p.program
	done := p.done
	p.mu.Unlock()

	// 通过 Bubble Tea 的 Quit 通知主循环优雅退出，而不是强杀终端界面。
	if program != nil {
		program.Quit()
	}
	if done == nil {
		return nil
	}

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
