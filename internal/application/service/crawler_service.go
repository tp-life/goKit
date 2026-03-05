package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"goKit/internal/domain/entity"
	"goKit/pkg/kit/db"

	"gorm.io/gorm/clause"
)

// =========================================================================
// 1. 结构体与初始化
// =========================================================================

// EastMoneyHoldingItem 东方财富持仓数据节点
type EastMoneyHoldingItem struct {
	SecurityCode     string   `json:"SECURITY_CODE"`
	SecurityNameAbbr string   `json:"SECURITY_NAME_ABBR"`
	EndDate          string   `json:"END_DATE"`
	HolderName       string   `json:"HOLDER_NAME"`
	HoldNum          *float64 `json:"HOLD_NUM"`           // 指针防空指针崩溃
	FreeHoldnumRatio *float64 `json:"FREE_HOLDNUM_RATIO"` // 指针防空指针崩溃
	HoldNumChange    any      `json:"HOLD_NUM_CHANGE"`
}

type CrawlerService struct {
	dbClient *db.Client
	logger   *slog.Logger
	client   *http.Client
}

func NewCrawlerService(dbClient *db.Client, logger *slog.Logger) *CrawlerService {
	return &CrawlerService{
		dbClient: dbClient,
		logger:   logger,
		// 全局复用 HTTP 客户端，配置防拥塞超时
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

// =========================================================================
// 2. 基础数据抓取 (新浪全量快照 + 新浪行业分类)
// =========================================================================

func (s *CrawlerService) SyncStockBasics(ctx context.Context) error {
	s.logger.Info("开始同步全市场股票列表及行业分类...")

	// 1. 获取行业映射字典 (Code -> Industry)
	industryMap := s.fetchSinaIndustryMap(ctx)

	// 2. 获取全量 A 股列表
	apiURL := "https://vip.stock.finance.sina.com.cn/quotes_service/api/json_v2.php/Market_Center.getHQNodeData?node=hs_a&pageSize=6000"
	body, err := s.httpGet(ctx, apiURL, "")
	if err != nil {
		return fmt.Errorf("拉取基础列表失败: %v", err)
	}

	var rawData []struct {
		Symbol string `json:"symbol"`
		Name   string `json:"name"`
	}
	if err := json.Unmarshal(body, &rawData); err != nil {
		return fmt.Errorf("解析基础列表失败: %v", err)
	}

	// 3. 批量写入数据库
	return s.dbClient.WithTx(ctx, func(txCtx context.Context) error {
		tx := s.dbClient.GetDB(txCtx)
		var batch []entity.StockInfo

		for _, item := range rawData {
			if len(item.Symbol) < 8 {
				continue
			}
			code := item.Symbol[2:] // 截取 sh600519 -> 600519
			batch = append(batch, entity.StockInfo{
				StockCode: code,
				StockName: item.Name,
				Exchange:  strings.ToUpper(item.Symbol[:2]), // 提取 SH/SZ
				Industry:  industryMap[code],                // 匹配行业
			})
		}

		if len(batch) == 0 {
			return nil
		}

		// 冲突时更新名称和行业
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "stock_code"}},
			DoUpdates: clause.AssignmentColumns([]string{"stock_name", "industry", "exchange", "updated_at"}),
		}).CreateInBatches(&batch, 1000).Error
	})
}

// =========================================================================
// 3. 行情数据抓取 (腾讯极速 K 线源)
// =========================================================================

func (s *CrawlerService) SyncDailyQuotes(ctx context.Context, code string, limit int) error {
	symbol := s.toTencentSymbol(code)
	// 腾讯接口：web.ifzq.gtimg.cn，速度极快且不封海外 IP
	apiURL := fmt.Sprintf("https://web.ifzq.gtimg.cn/app/kline/get?_var=kline_day&symbol=%s&type=last&n=%d", symbol, limit)

	body, err := s.httpGet(ctx, apiURL, "https://gu.qq.com")
	if err != nil {
		return err
	}

	// 腾讯返回的是纯文本附带 JSON： kline_day=[{"data":{"day":[["2024-03-01","10.0","10.5"...]]}}]
	content := string(body)
	start := strings.Index(content, "=[")
	if start == -1 {
		return fmt.Errorf("腾讯 K 线数据格式异常")
	}

	var resp struct {
		Data struct {
			Day [][]string `json:"day"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(content[start+1:]), &resp); err != nil {
		return fmt.Errorf("解析腾讯 K 线失败: %v", err)
	}

	// 批量写入
	return s.dbClient.WithTx(ctx, func(txCtx context.Context) error {
		tx := s.dbClient.GetDB(txCtx)
		var batch []entity.StockDailyQuote

		for _, q := range resp.Data.Day {
			if len(q) < 6 {
				continue
			}
			t, _ := time.Parse("2006-01-02", q[0])
			open, _ := strconv.ParseFloat(q[1], 64)
			closeVal, _ := strconv.ParseFloat(q[2], 64)
			high, _ := strconv.ParseFloat(q[3], 64)
			low, _ := strconv.ParseFloat(q[4], 64)
			vol, _ := strconv.ParseInt(q[5], 10, 64)

			batch = append(batch, entity.StockDailyQuote{
				StockCode: code, TradeDate: t, Open: open, Close: closeVal, High: high, Low: low, Volume: vol,
			})
		}

		if len(batch) == 0 {
			return nil
		}

		return tx.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(&batch, 500).Error
	})
}

// =========================================================================
// 4. 持仓数据抓取 (东方财富数据中心)
// =========================================================================

func (s *CrawlerService) SyncHoldings(ctx context.Context, reportDate string) error {
	s.logger.Info("开始分页同步全市场持仓流水...", slog.String("date", reportDate))
	page := 1
	totalRecords := 0

	for {
		apiURL := "https://datacenter-web.eastmoney.com/api/data/v1/get"
		params := url.Values{}
		params.Add("pageSize", "500") // 每次拉取 500 条
		params.Add("pageNumber", strconv.Itoa(page))
		params.Add("reportName", "RPT_F10_EH_FREEHOLDERS")
		params.Add("columns", "SECURITY_CODE,SECURITY_NAME_ABBR,END_DATE,HOLDER_NAME,HOLD_NUM,FREE_HOLDNUM_RATIO,HOLD_NUM_CHANGE")
		params.Add("filter", fmt.Sprintf(`(END_DATE='%s')`, reportDate))

		body, err := s.httpGet(ctx, apiURL+"?"+params.Encode(), "")
		if err != nil {
			s.logger.Error("拉取持仓失败", slog.Int("page", page), slog.Any("err", err))
			break
		}

		var emResp struct {
			Result struct {
				Pages int                    `json:"pages"`
				Data  []EastMoneyHoldingItem `json:"data"`
			} `json:"result"`
			Success bool `json:"success"`
		}

		if err := json.Unmarshal(body, &emResp); err != nil {
			break
		}

		if !emResp.Success || len(emResp.Result.Data) == 0 {
			break
		}

		// 执行批量存储核心逻辑
		if err := s.saveHoldingsBatch(ctx, emResp.Result.Data); err != nil {
			s.logger.Error("入库失败", slog.Int("page", page), slog.Any("err", err))
		} else {
			totalRecords += len(emResp.Result.Data)
			s.logger.Info("持仓分页拉取完成", slog.Int("page", page), slog.Int("totalPages", emResp.Result.Pages))
		}

		if page >= emResp.Result.Pages {
			break
		}
		page++
		time.Sleep(300 * time.Millisecond) // 防封控缓冲
	}

	s.logger.Info("持仓同步彻底完成", slog.Int("总新增流水记录", totalRecords))
	return nil
}

// saveHoldingsBatch 持仓批量存储核心逻辑 (完全无 N+1 查询)
func (s *CrawlerService) saveHoldingsBatch(ctx context.Context, data []EastMoneyHoldingItem) error {
	return s.dbClient.WithTx(ctx, func(txCtx context.Context) error {
		tx := s.dbClient.GetDB(txCtx)

		// 1. 抽取当前批次中所有独特的机构名称
		uniqueInstNames := make(map[string]bool)
		var instBatch []entity.InstitutionInfo

		for _, item := range data {
			if item.HolderName == "" || item.SecurityCode == "" {
				continue
			}
			if !uniqueInstNames[item.HolderName] {
				uniqueInstNames[item.HolderName] = true
				instBatch = append(instBatch, entity.InstitutionInfo{
					InstName: item.HolderName,
					InstType: s.identifyInstType(item.HolderName), // 自动识别国家队
				})
			}
		}

		// 2. 批量 UPSERT 机构数据 (忽略已存在的)
		if len(instBatch) > 0 {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(&instBatch, len(instBatch)).Error; err != nil {
				return fmt.Errorf("批量写入机构维度表失败: %v", err)
			}
		}

		// 3. 将机构名和对应的自增 ID 映射到内存中 (消除逐条查询的 N+1 性能瓶颈)
		var namesList []string
		for name := range uniqueInstNames {
			namesList = append(namesList, name)
		}
		var existingInsts []entity.InstitutionInfo
		tx.Select("inst_id, inst_name").Where("inst_name IN ?", namesList).Find(&existingInsts)

		instIDMap := make(map[string]uint64)
		for _, inst := range existingInsts {
			instIDMap[inst.InstName] = inst.InstID
		}

		// 4. 构建持仓流水批次
		var recordBatch []entity.StockHoldingRecord
		for _, item := range data {
			instID, exists := instIDMap[item.HolderName]
			if !exists {
				continue
			}

			date, err := time.Parse("2006-01-02", item.EndDate[:10])
			if err != nil {
				continue
			}

			recordBatch = append(recordBatch, entity.StockHoldingRecord{
				StockCode:  item.SecurityCode,
				InstID:     instID,
				ReportDate: date,
				HoldCount:  int64(s.derefFloat(item.HoldNum)),
				HoldRatio:  s.derefFloat(item.FreeHoldnumRatio),
				ChangeType: fmt.Sprintf("%v", item.HoldNumChange),
			})
		}

		// 5. 批量写入持仓流水 (相同股票、同一天、同一机构，则忽略)
		if len(recordBatch) > 0 {
			// 明确告诉 GORM 我们的联合唯一键是哪三个字段，MySQL 遇到冲突时会优雅地 Ignore
			err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{
					{Name: "stock_code"},
					{Name: "inst_id"},
					{Name: "report_date"},
				},
				DoNothing: true,
			}).CreateInBatches(&recordBatch, len(recordBatch)).Error

			if err != nil {
				return fmt.Errorf("批量写入持仓流水失败: %v", err)
			}
		}

		return nil
	})
}

// =========================================================================
// 5. 原子化底层辅助方法
// =========================================================================

// httpGet 统一网络请求封装
func (s *CrawlerService) httpGet(ctx context.Context, targetURL, referer string) ([]byte, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120.0.0.0")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// fetchSinaIndustryMap 获取新浪行业板块映射
func (s *CrawlerService) fetchSinaIndustryMap(ctx context.Context) map[string]string {
	resultMap := make(map[string]string)

	// 1. 获取行业大类节点
	nodeResp, err := s.httpGet(ctx, "https://vip.stock.finance.sina.com.cn/quotes_service/api/json_v2.php/Market_Center.getHQNodes", "")
	if err != nil {
		s.logger.Warn("获取新浪行业节点失败", slog.Any("err", err))
		return resultMap
	}

	var nodes [][]string
	if err := json.Unmarshal(nodeResp, &nodes); err != nil {
		return resultMap
	}

	// 2. 遍历大类获取内部股票
	for _, node := range nodes {
		nodeID, nodeName := node[0], node[1]
		if !strings.HasPrefix(nodeID, "hangye_") { // 仅筛选纯行业
			continue
		}

		stockURL := fmt.Sprintf("https://vip.stock.finance.sina.com.cn/quotes_service/api/json_v2.php/Market_Center.getHQNodeData?node=%s&pageSize=1000", nodeID)
		stockBody, err := s.httpGet(ctx, stockURL, "")
		if err != nil {
			continue
		}

		var list []struct {
			Symbol string `json:"symbol"`
		}
		if json.Unmarshal(stockBody, &list) == nil {
			for _, st := range list {
				if len(st.Symbol) >= 8 {
					resultMap[st.Symbol[2:]] = nodeName
				}
			}
		}
		time.Sleep(100 * time.Millisecond) // 防止并发过高
	}

	return resultMap
}

// identifyInstType 核心打标器：识别国家队
func (s *CrawlerService) identifyInstType(name string) string {
	keywords := []string{"中央汇金", "证金公司", "中国证券金融", "社保基金", "基本养老", "梧桐树"}
	for _, k := range keywords {
		if strings.Contains(name, k) {
			return "国家队"
		}
	}
	return "普通机构"
}

// toTencentSymbol A 股代码转腾讯格式
func (s *CrawlerService) toTencentSymbol(code string) string {
	if strings.HasPrefix(code, "6") || strings.HasPrefix(code, "9") {
		return "sh" + code
	}
	if strings.HasPrefix(code, "4") || strings.HasPrefix(code, "8") {
		return "bj" + code
	}
	return "sz" + code
}

// derefFloat 安全解引用浮点指针
func (s *CrawlerService) derefFloat(f *float64) float64 {
	if f == nil {
		return 0
	}
	return *f
}
