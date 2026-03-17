package exchange

// 旧版的 PublicClient 已经被拆分为：
// 1. MarketAdapter：公共行情/交易对/资金费率/盘口
// 2. TradeAdapter：下单/平仓/查持仓
//
// 这个文件保留为空实现，只是为了明确完成了架构替换，
// 避免旧版 client.go 中的重复定义继续参与编译。
