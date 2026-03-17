package internal

import (
	"goKit/internal/application/service"
	"goKit/internal/infrastructure/exchange"
	"goKit/internal/infrastructure/persistence"
	httpHandler "goKit/internal/interface/http/handler"
	httpRouter "goKit/internal/interface/http/router"

	"go.uber.org/fx"
)

var Module = fx.Options(
	fx.Provide(
		persistence.NewSymbolRepository,
		persistence.NewMarketDataRepository,
		persistence.NewOpportunityRepository,
		persistence.NewExecutionPlanRepository,
		persistence.NewExecutionRepository,
		persistence.NewOrderRepository,

		service.NewMarketStore,

		fx.Annotate(exchange.NewBinanceMarketClient, fx.ResultTags(`group:"markets"`)),
		fx.Annotate(exchange.NewAsterMarketClient, fx.ResultTags(`group:"markets"`)),
		fx.Annotate(exchange.NewHyperliquidMarketClient, fx.ResultTags(`group:"markets"`)),

		fx.Annotate(exchange.NewBinanceTradeClient, fx.ResultTags(`group:"trades"`)),
		fx.Annotate(exchange.NewAsterTradeClient, fx.ResultTags(`group:"trades"`)),
		fx.Annotate(exchange.NewHyperliquidTradeClient, fx.ResultTags(`group:"trades"`)),

		service.NewSymbolService,
		service.NewOpportunityQueryService,
		service.NewExecutionPlanService,
		service.NewExecutionService,
		service.NewMarketQueryService,
		service.NewSystemService,
		service.NewStrategyRunner,
		service.NewSnapshotStatsService,

		httpHandler.NewSnapshotStatsHandler,
		httpHandler.NewHealthHandler,
		httpHandler.NewSymbolHandler,
		httpHandler.NewOpportunityHandler,
		httpHandler.NewExecutionPlanHandler,
		httpHandler.NewExecutionHandler,
		httpHandler.NewMarketHandler,
		httpHandler.NewSystemHandler,
		httpRouter.NewRouter,
	),
	fx.Invoke(
		persistence.AutoMigrate,
		service.StartStrategyRunner,
		service.StartExecutionEngine,
	),
)
