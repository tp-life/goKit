package polymarket

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

// FlexibleFloat 是一个宽松的浮点字段，兼容 number、string 和 null 三种 JSON 形态。
type FlexibleFloat struct {
	Valid bool
	Value float64
}

// FlexibleInt 是一个宽松的整数字段，兼容 number、string 和 null 三种 JSON 形态。
type FlexibleInt struct {
	Valid bool
	Value int
}

// UnmarshalJSON 负责把多种 JSON 输入形式归一化成 float64。
func (f *FlexibleFloat) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(bytes.TrimSpace(data)))
	if raw == "" || raw == "null" {
		f.Valid = false
		f.Value = 0
		return nil
	}

	// 先尝试按字符串解析，兼容服务端偶尔返回的字符串数字。
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		text = strings.TrimSpace(text)
		if text == "" {
			f.Valid = false
			f.Value = 0
			return nil
		}
		if v, err := strconv.ParseFloat(text, 64); err == nil {
			f.Valid = true
			f.Value = v
			return nil
		}
		f.Valid = false
		f.Value = 0
		return nil
	}

	// 再按普通数字解析，覆盖标准 JSON number。
	var value float64
	if err := json.Unmarshal(data, &value); err == nil {
		f.Valid = true
		f.Value = value
		return nil
	}

	f.Valid = false
	f.Value = 0
	return nil
}

// Ptr 返回一个可安全复用的 float64 指针；无值时返回 nil。
func (f FlexibleFloat) Ptr() *float64 {
	if !f.Valid {
		return nil
	}
	value := f.Value
	return &value
}

// OrZero 返回字段值；无值时回退为 0。
func (f FlexibleFloat) OrZero() float64 {
	if !f.Valid {
		return 0
	}
	return f.Value
}

// UnmarshalJSON 负责把多种 JSON 输入形式归一化成 int。
func (f *FlexibleInt) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(bytes.TrimSpace(data)))
	if raw == "" || raw == "null" {
		f.Valid = false
		f.Value = 0
		return nil
	}

	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		text = strings.TrimSpace(text)
		if text == "" {
			f.Valid = false
			f.Value = 0
			return nil
		}
		if v, err := strconv.Atoi(text); err == nil {
			f.Valid = true
			f.Value = v
			return nil
		}
		f.Valid = false
		f.Value = 0
		return nil
	}

	var value int
	if err := json.Unmarshal(data, &value); err == nil {
		f.Valid = true
		f.Value = value
		return nil
	}

	var floatValue float64
	if err := json.Unmarshal(data, &floatValue); err == nil {
		f.Valid = true
		f.Value = int(floatValue)
		return nil
	}

	f.Valid = false
	f.Value = 0
	return nil
}

// OrZero 返回字段值；无值时回退为 0。
func (f FlexibleInt) OrZero() int {
	if !f.Valid {
		return 0
	}
	return f.Value
}

// FlexibleText 是一个宽松的文本字段，兼容 string、number、bool 和 null。
type FlexibleText string

// UnmarshalJSON 负责把多种 JSON 标量统一转成字符串。
func (t *FlexibleText) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(bytes.TrimSpace(data)))
	if raw == "" || raw == "null" {
		*t = ""
		return nil
	}

	// 标准字符串优先走 JSON 反序列化，保留转义语义。
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*t = FlexibleText(strings.TrimSpace(text))
		return nil
	}

	// 对 number/bool 等非字符串标量，直接使用文本形式。
	*t = FlexibleText(strings.Trim(strings.TrimSpace(raw), `"`))
	return nil
}

// String 返回清理过空白字符后的字符串值。
func (t FlexibleText) String() string {
	return strings.TrimSpace(string(t))
}

// DataPositionResponse 表示 Data API `/positions` 返回的持仓条目。
type DataPositionResponse struct {
	ProxyWallet string        `json:"proxyWallet,omitempty"`
	Asset       string        `json:"asset,omitempty"`
	ConditionID string        `json:"conditionId,omitempty"`
	Slug        string        `json:"slug,omitempty"`
	EventSlug   string        `json:"eventSlug,omitempty"`
	Title       string        `json:"title,omitempty"`
	Outcome     string        `json:"outcome,omitempty"`
	Side        string        `json:"side,omitempty"`
	Size        FlexibleFloat `json:"size"`
	AvgPrice    FlexibleFloat `json:"avgPrice"`
	CurPrice    FlexibleFloat `json:"curPrice"`
	RealizedPnL FlexibleFloat `json:"realizedPnl"`
	Redeemable  bool          `json:"redeemable,omitempty"`
	Mergeable   bool          `json:"mergeable,omitempty"`
}

// DataClosedPositionResponse 表示 Data API `/closed-positions` 返回的已关闭仓位条目。
type DataClosedPositionResponse struct {
	ID              FlexibleText  `json:"id"`
	TransactionHash FlexibleText  `json:"transactionHash"`
	EndDate         FlexibleText  `json:"endDate"`
	Timestamp       FlexibleText  `json:"timestamp"`
	UpdatedAt       FlexibleText  `json:"updatedAt"`
	Slug            string        `json:"slug,omitempty"`
	MarketSlug      string        `json:"marketSlug,omitempty"`
	Question        string        `json:"question,omitempty"`
	Outcome         string        `json:"outcome,omitempty"`
	Side            string        `json:"side,omitempty"`
	PositionSide    string        `json:"positionSide,omitempty"`
	AvgPrice        FlexibleFloat `json:"avgPrice"`
	AvgPriceAlt     FlexibleFloat `json:"avg_price"`
	Size            FlexibleFloat `json:"size"`
	RealizedPnL     FlexibleFloat `json:"realizedPnl"`
	RealizedPnLAlt  FlexibleFloat `json:"realized_pnl"`
}

// DataActivityResponse 表示 Data API `/activity` 返回的一条原始活动记录。
type DataActivityResponse struct {
	ID              FlexibleText  `json:"id"`
	TradeID         FlexibleText  `json:"tradeID"`
	TransactionHash FlexibleText  `json:"transactionHash"`
	Type            FlexibleText  `json:"type"`
	Side            FlexibleText  `json:"side"`
	Outcome         string        `json:"outcome,omitempty"`
	Direction       string        `json:"direction,omitempty"`
	Price           FlexibleFloat `json:"price"`
	SizeMatched     FlexibleFloat `json:"size_matched"`
	Size            FlexibleFloat `json:"size"`
	OriginalSize    FlexibleFloat `json:"original_size"`
	USDCSize        FlexibleFloat `json:"usdcSize"`
	USDCSizeAlt     FlexibleFloat `json:"usdc_size"`
	ConditionID     FlexibleText  `json:"conditionId"`
	ConditionIDAlt  FlexibleText  `json:"condition_id"`
	Market          FlexibleText  `json:"market"`
	MarketID        FlexibleText  `json:"market_id"`
	EventSlug       string        `json:"eventSlug,omitempty"`
	Slug            string        `json:"slug,omitempty"`
	AssetID         FlexibleText  `json:"asset_id"`
	Asset           FlexibleText  `json:"asset"`
	TokenID         FlexibleText  `json:"token_id"`
	Title           string        `json:"title,omitempty"`
	EventTitle      string        `json:"eventTitle,omitempty"`
	Name            string        `json:"name,omitempty"`
	Question        string        `json:"question,omitempty"`
	MatchTime       FlexibleText  `json:"matchtime"`
	MatchTimeAlt    FlexibleText  `json:"match_time"`
	Timestamp       FlexibleText  `json:"timestamp"`
	CreatedAt       FlexibleText  `json:"created_at"`
	Time            FlexibleText  `json:"time"`
}
