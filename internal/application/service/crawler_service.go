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

type EastMoneyHoldingItem struct {
	SecurityCode     string   `json:"SECURITY_CODE"`
	SecurityNameAbbr string   `json:"SECURITY_NAME_ABBR"`
	EndDate          string   `json:"END_DATE"`
	HolderName       string   `json:"HOLDER_NAME"`
	HoldNum          *float64 `json:"HOLD_NUM"`
	FreeHoldnumRatio *float64 `json:"FREE_HOLDNUM_RATIO"`
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
		client:   &http.Client{Timeout: 20 * time.Second},
	}
}

// =========================================================================
// 2. 基础数据抓取 (新浪全市场真实分页扫描)
// =========================================================================

func (s *CrawlerService) SyncStockBasics(ctx context.Context) error {
	s.logger.Info("开始同步全市场股票列表及行业分类...")

	industryMap := s.fetchSinaIndustryMap(ctx)

	var allRawData []struct {
		Symbol string `json:"symbol"`
		Name   string `json:"name"`
	}

	nodes := []string{"hs_a", "bjs_a"}
	for _, node := range nodes {
		page := 1
		for {
			apiURL := fmt.Sprintf("https://vip.stock.finance.sina.com.cn/quotes_service/api/json_v2.php/Market_Center.getHQNodeData?page=%d&num=100&sort=symbol&asc=1&node=%s", page, node)
			body, err := s.httpGet(ctx, apiURL, "")
			if err != nil {
				break
			}

			var pageData []struct {
				Symbol string `json:"symbol"`
				Name   string `json:"name"`
			}
			if err := json.Unmarshal(body, &pageData); err != nil || len(pageData) == 0 {
				break
			}

			allRawData = append(allRawData, pageData...)

			if len(pageData) < 100 {
				break
			}
			page++
			time.Sleep(30 * time.Millisecond)
		}
	}

	s.logger.Info("成功从新浪拉取到全量股票代码", slog.Int("count", len(allRawData)))

	return s.dbClient.WithTx(ctx, func(txCtx context.Context) error {
		tx := s.dbClient.GetDB(txCtx)
		var batch []entity.StockInfo

		for _, item := range allRawData {
			if len(item.Symbol) < 8 {
				continue
			}
			code := item.Symbol[2:]
			batch = append(batch, entity.StockInfo{
				StockCode: code,
				StockName: item.Name,
				Exchange:  strings.ToUpper(item.Symbol[:2]),
				Industry:  industryMap[code],
			})
		}

		if len(batch) == 0 {
			return nil
		}

		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "stock_code"}},
			DoUpdates: clause.AssignmentColumns([]string{"stock_name", "industry", "exchange", "updated_at"}),
		}).CreateInBatches(&batch, 1000).Error
	})
}

// =========================================================================
// 3. 行情数据抓取 (全面替换为：东方财富行情源)
// =========================================================================

func (s *CrawlerService) SyncAllDailyQuotes(ctx context.Context, limit int) error {
	s.logger.Info("开始全量同步 A 股 K 线数据 (数据源: 东方财富)...")

	var stocks []entity.StockInfo
	if err := s.dbClient.GetDB(ctx).Select("stock_code").Find(&stocks).Error; err != nil {
		return fmt.Errorf("查询股票列表失败: %v", err)
	}

	total := len(stocks)
	successCount := 0

	for i, stock := range stocks {
		err := s.SyncDailyQuotes(ctx, stock.StockCode, limit)
		if err == nil {
			successCount++
		} else {
			s.logger.Debug("单只股票 K 线同步跳过", slog.String("code", stock.StockCode), slog.Any("err", err))
		}

		if (i+1)%500 == 0 {
			s.logger.Info("K 线全量同步进度", slog.Int("current", i+1), slog.Int("total", total), slog.Int("success", successCount))
		}

		time.Sleep(20 * time.Millisecond)
	}

	s.logger.Info("全量 K 线同步彻底完成", slog.Int("成功数量", successCount), slog.Int("总数", total))
	return nil
}

func (s *CrawlerService) SyncDailyQuotes(ctx context.Context, code string, limit int) error {
	secid := s.toEastMoneySecID(code)

	// 东方财富 K 线接口
	// klt=101 (日线), fqt=1 (前复权), lmt=获取条数
	apiURL := fmt.Sprintf("https://push2his.eastmoney.com/api/qt/stock/kline/get?secid=%s&fields1=f1,f2,f3,f4,f5,f6&fields2=f51,f52,f53,f54,f55,f56&klt=101&fqt=1&end=20500101&lmt=%d", secid, limit)

	body, err := s.httpGet(ctx, apiURL, "")
	if err != nil {
		return err
	}

	var resp struct {
		Data struct {
			Klines []string `json:"klines"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("JSON解析失败: %v", err)
	}

	if len(resp.Data.Klines) == 0 {
		return fmt.Errorf("暂无有效 K 线数据")
	}

	return s.dbClient.WithTx(ctx, func(txCtx context.Context) error {
		tx := s.dbClient.GetDB(txCtx)
		var batch []entity.StockDailyQuote

		for _, line := range resp.Data.Klines {
			// 东财格式: "日期,开盘,收盘,最高,最低,成交量(手),成交额"
			// 例如: "2024-03-01,10.00,10.50,10.60,9.90,123456,123456789.00"
			parts := strings.Split(line, ",")
			if len(parts) < 6 {
				continue
			}

			t, err := time.Parse("2006-01-02", parts[0])
			if err != nil {
				continue
			}

			open, _ := strconv.ParseFloat(parts[1], 64)
			closeVal, _ := strconv.ParseFloat(parts[2], 64)
			high, _ := strconv.ParseFloat(parts[3], 64)
			low, _ := strconv.ParseFloat(parts[4], 64)
			vol, _ := strconv.ParseFloat(parts[5], 64)

			batch = append(batch, entity.StockDailyQuote{
				StockCode: code,
				TradeDate: t,
				Open:      open,
				Close:     closeVal,
				High:      high,
				Low:       low,
				Volume:    int64(vol),
			})
		}

		if len(batch) == 0 {
			return nil
		}

		return tx.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(&batch, 500).Error
	})
}

// =========================================================================
// 4. 持仓数据抓取 (东方财富 - 按股票遍历规避截断问题)
// =========================================================================

func (s *CrawlerService) SyncHoldings(ctx context.Context, reportDate string) error {
	var stocks []string
	if err := s.dbClient.GetDB(ctx).Model(&entity.StockInfo{}).Pluck("stock_code", &stocks).Error; err != nil {
		return err
	}

	s.logger.Info("开始按个股循环同步持仓 (修复防盗链与排序校验)...", slog.Int("stockCount", len(stocks)))
	totalRecords := 0

	for i, code := range stocks {
		apiURL := "https://datacenter-web.eastmoney.com/api/data/v1/get"
		params := url.Values{}
		params.Add("pageSize", "50")
		params.Add("pageNumber", "1")
		params.Add("reportName", "RPT_F10_EH_FREEHOLDERS")
		params.Add("columns", "SECURITY_CODE,SECURITY_NAME_ABBR,END_DATE,HOLDER_NAME,HOLD_NUM,FREE_HOLDNUM_RATIO,HOLD_NUM_CHANGE")
		params.Add("filter", fmt.Sprintf(`(SECURITY_CODE="%s")(END_DATE='%s')`, code, reportDate))

		// 【修复点 1】：强行增加排序字段（按持股数倒序），东财接口不传此参数通常会返回 Null
		params.Add("sortColumns", "HOLD_NUM")
		params.Add("sortTypes", "-1")
		// 【修复点 2】：增加终端标识，模拟真实网页请求
		params.Add("source", "WEB")
		params.Add("client", "WEB")

		// 【修复点 3】：必须加上东方财富的专属 Referer，否则触发防盗链机制返回空数据！
		referer := "https://data.eastmoney.com/"
		body, err := s.httpGet(ctx, apiURL+"?"+params.Encode(), referer)
		if err != nil {
			continue
		}

		var emResp struct {
			Result struct {
				Data []EastMoneyHoldingItem `json:"data"`
			} `json:"result"`
			Success bool `json:"success"`
		}

		if err := json.Unmarshal(body, &emResp); err == nil && emResp.Success && emResp.Result.Data != nil && len(emResp.Result.Data) > 0 {
			if err := s.saveHoldingsBatch(ctx, emResp.Result.Data); err == nil {
				totalRecords += len(emResp.Result.Data)
			}
		}
		s.logger.Info("saveHoldingsBatch xxxx", slog.String("code", code), slog.Int("records", len(emResp.Result.Data)))
		// 打印进度
		if (i+1)%500 == 0 {
			s.logger.Info("持仓同步进度", slog.Int("current", i+1), slog.Int("total", len(stocks)), slog.Int("savedRecords", totalRecords))
		}

		// 稍微延长一点休眠，东方财富对单 IP 频率限制比腾讯严苛一点
		time.Sleep(20 * time.Millisecond)
	}

	s.logger.Info("全市场持仓同步彻底完成", slog.Int("总流水", totalRecords))
	return nil
}

func (s *CrawlerService) saveHoldingsBatch(ctx context.Context, data []EastMoneyHoldingItem) error {
	return s.dbClient.WithTx(ctx, func(txCtx context.Context) error {
		tx := s.dbClient.GetDB(txCtx)

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
					InstType: s.identifyInstType(item.HolderName),
				})
			}
		}

		if len(instBatch) > 0 {
			tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(&instBatch, len(instBatch))
		}

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

		if len(recordBatch) > 0 {
			err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{
					{Name: "stock_code"},
					{Name: "inst_id"},
					{Name: "report_date"},
				},
				DoNothing: true,
			}).CreateInBatches(&recordBatch, len(recordBatch)).Error

			if err != nil {
				return err
			}
		}

		return nil
	})
}

// =========================================================================
// 5. 原子化底层辅助方法
// =========================================================================

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

func (s *CrawlerService) fetchSinaIndustryMap(ctx context.Context) map[string]string {
	resultMap := make(map[string]string)
	nodeResp, err := s.httpGet(ctx, "https://vip.stock.finance.sina.com.cn/quotes_service/api/json_v2.php/Market_Center.getHQNodes", "")
	if err != nil {
		return resultMap
	}

	var nodes [][]string
	if err := json.Unmarshal(nodeResp, &nodes); err != nil {
		return resultMap
	}

	for _, node := range nodes {
		nodeID, nodeName := node[0], node[1]
		if !strings.HasPrefix(nodeID, "hangye_") {
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
		time.Sleep(50 * time.Millisecond)
	}
	return resultMap
}

func (s *CrawlerService) identifyInstType(name string) string {
	keywords := []string{"中央汇金", "证金公司", "中国证券金融", "社保基金", "基本养老", "梧桐树"}
	for _, k := range keywords {
		if strings.Contains(name, k) {
			return "国家队"
		}
	}
	return "普通机构"
}

// toEastMoneySecID 转换股票代码为东方财富的市场ID格式 (1=沪, 0=深/北)
func (s *CrawlerService) toEastMoneySecID(code string) string {
	// 沪市 A 股、科创板通常是 6 开头
	if strings.HasPrefix(code, "6") {
		return "1." + code
	}
	// 深市(0, 3开头) 和 北交所(4, 8, 92开头) 在东财的标号中均算作 0
	return "0." + code
}

func (s *CrawlerService) derefFloat(f *float64) float64 {
	if f == nil {
		return 0
	}
	return *f
}
