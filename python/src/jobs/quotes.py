import time
from datetime import datetime

import akshare as ak
import pandas as pd
from sqlalchemy import text

from src.db import get_db_connection


def get_national_team_stocks() -> list:
    with get_db_connection() as conn:
        result = conn.execute(
            text("""
            SELECT DISTINCT s.stock_code
            FROM stock_holding_records s
            JOIN institution_info i ON s.inst_id = i.inst_id
            WHERE i.inst_type = '国家队'
        """)
        )
        return [row[0] for row in result]


def sync_daily_quotes(start_date: str, end_date: str):
    stocks = get_national_team_stocks()
    print(
        f"共发现 {len(stocks)} 只国家队持仓股票，开始拉取行情 ({start_date} ~ {end_date})..."
    )

    total_inserted = 0
    with get_db_connection() as conn:
        for i, stock_code in enumerate(stocks):
            try:
                df = ak.stock_zh_a_hist(
                    symbol=stock_code,
                    period="daily",
                    start_date=start_date,
                    end_date=end_date,
                    adjust="qq",
                )
                if df is None or df.empty:
                    continue

                for _, row in df.iterrows():
                    trade_date = pd.to_datetime(row["日期"]).strftime("%Y-%m-%d")
                    res = conn.execute(
                        text("""
                            INSERT IGNORE INTO stock_daily_quotes
                            (stock_code, trade_date, open, close, high, low, volume, turnover_rate, created_at)
                            VALUES (:code, :date, :open, :close, :high, :low, :vol, :turnover, NOW())
                        """),
                        {
                            "code": stock_code,
                            "date": trade_date,
                            "open": float(row["开盘"]),
                            "close": float(row["收盘"]),
                            "high": float(row["最高"]),
                            "low": float(row["最低"]),
                            "vol": int(row["成交量"]),
                            "turnover": float(row["换手率"]),
                        },
                    )
                    total_inserted += res.rowcount

                time.sleep(0.3)  # 防封 IP
                if (i + 1) % 50 == 0:
                    print(f"进度: {i + 1} / {len(stocks)}")

            except Exception as e:
                print(f"获取 {stock_code} 行情失败: {e}")

    print(f"[{datetime.now()}] 行情同步完成！新增 K 线数据: {total_inserted} 条")
