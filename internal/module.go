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

		fx.Annotate(exchange.DefaultAdapterFactories, fx.ResultTags(`group:"exchange_adapter_factories,flatten"`)),
		exchange.NewProvidedAdapterRegistry,
		fx.Annotate(exchange.ProvideMarketAdapters, fx.ResultTags(`group:"markets,flatten"`)),
		fx.Annotate(exchange.ProvideTradeAdapters, fx.ResultTags(`group:"trades,flatten"`)),

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
