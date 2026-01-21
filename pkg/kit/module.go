package kit

import (
	"goKit/pkg/kit/db"
	"goKit/pkg/kit/log"
	"goKit/pkg/kit/web"

	"go.uber.org/fx"
)

var Module = fx.Options(
	// 1. 优先提供 Logger (因为其他组件都依赖它)
	fx.Provide(log.NewLogger),
	fx.Provide(db.NewClient),
	fx.Provide(web.NewServer),
	fx.Invoke(web.StartLifecycle),
)
