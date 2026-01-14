package math

import (
	"github.com/shopspring/decimal"
)

// Add 加法
func Add(a, b float64) float64 {
	d1 := decimal.NewFromFloat(a)
	d2 := decimal.NewFromFloat(b)
	result, _ := d1.Add(d2).Float64()
	return result
}

// Sub 减法
func Sub(a, b float64) float64 {
	d1 := decimal.NewFromFloat(a)
	d2 := decimal.NewFromFloat(b)
	result, _ := d1.Sub(d2).Float64()
	return result
}

// Mul 乘法
func Mul(a, b float64) float64 {
	d1 := decimal.NewFromFloat(a)
	d2 := decimal.NewFromFloat(b)
	result, _ := d1.Mul(d2).Float64()
	return result
}

// Div 除法
func Div(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	d1 := decimal.NewFromFloat(a)
	d2 := decimal.NewFromFloat(b)
	result, _ := d1.Div(d2).Float64()
	return result
}

// Abs 绝对值
func Abs(a float64) float64 {
	d := decimal.NewFromFloat(a)
	if d.IsNegative() {
		d = d.Neg()
	}
	result, _ := d.Float64()
	return result
}

// Percentage 计算百分比
func Percentage(value, total float64) float64 {
	if total == 0 {
		return 0
	}
	return Div(Mul(value, 100), total)
}

// PriceDiff 计算价格差异（绝对值）
func PriceDiff(price1, price2 float64) float64 {
	return Abs(Sub(price1, price2))
}

// RateDiff 计算费率差异
func RateDiff(rate1, rate2 float64) float64 {
	return Sub(rate1, rate2)
}

// Profitability 计算预期收益率（扣除手续费后）
func Profitability(rateDiff, tradingFee1, tradingFee2 float64) float64 {
	grossProfit := Abs(rateDiff)
	totalFees := Add(tradingFee1, tradingFee2)
	return Sub(grossProfit, totalFees)
}
