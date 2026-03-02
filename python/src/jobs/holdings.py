from datetime import datetime

import akshare as ak
import pandas as pd
from sqlalchemy import text

from src.db import get_db_connection

NATIONAL_TEAM_KEYWORDS = [
    "中央汇金",
    "中国证券金融",
    "梧桐树",
    "凤山投资",
    "坤藤投资",
    "社保基金",
]


def check_inst_type(shareholder_name: str) -> str:
    for keyword in NATIONAL_TEAM_KEYWORDS:
        if keyword in shareholder_name:
            if "社保" in keyword:
                return "社保"
            return "国家队"
    return "其他"


def sync_quarterly_holdings(report_date: str):
    print(f"[{datetime.now()}] 开始拉取 {report_date} 财报期数据...")

    df = ak.stock_gdfx_top_10_em(date=report_date)
    if df is None or df.empty:
        print(f"未能获取到 {report_date} 的数据。")
        return

    formatted_date = f"{report_date[:4]}-{report_date[4:6]}-{report_date[6:8]}"
    inserted_count = 0

    with get_db_connection() as conn:
        print("1/3 同步股票维度表...")
        stocks = df[["股票代码", "股票简称"]].drop_duplicates()
        for _, row in stocks.iterrows():
            conn.execute(
                text(
                    "INSERT IGNORE INTO stock_info (stock_code, stock_name, created_at, updated_at) VALUES (:code, :name, NOW(), NOW())"
                ),
                {"code": row["股票代码"], "name": row["股票简称"]},
            )

        print("2/3 同步机构维度表...")
        institutions = df[["股东名称"]].drop_duplicates()
        for _, row in institutions.iterrows():
            inst_name = row["股东名称"]
            inst_type = check_inst_type(inst_name)
            conn.execute(
                text(
                    "INSERT IGNORE INTO institution_info (inst_name, inst_type, created_at, updated_at) VALUES (:name, :type, NOW(), NOW())"
                ),
                {"name": inst_name, "type": inst_type},
            )

        # 构建机构 ID 映射
        inst_mapping_df = pd.read_sql(
            "SELECT inst_id, inst_name FROM institution_info", conn.connection
        )
        inst_dict = dict(zip(inst_mapping_df["inst_name"], inst_mapping_df["inst_id"]))

        print("3/3 同步持仓明细事实表...")
        for _, row in df.iterrows():
            inst_id = inst_dict.get(row["股东名称"])
            if not inst_id:
                continue

            change_count = (
                int(row.get("增减持股数", 0))
                if pd.notna(row.get("增减持股数"))
                and str(row.get("增减持股数")).isdigit()
                else 0
            )

            res = conn.execute(
                text("""
                    INSERT IGNORE INTO stock_holding_records
                    (stock_code, inst_id, report_date, hold_count, hold_ratio, change_type, change_count, created_at)
                    VALUES (:code, :inst_id, :date, :count, :ratio, :c_type, :c_count, NOW())
                """),
                {
                    "code": row["股票代码"],
                    "inst_id": inst_id,
                    "date": formatted_date,
                    "count": int(row["持股数量"]),
                    "ratio": float(row["占总流通股本比例"]),
                    "c_type": row["增减持情况"],
                    "c_count": change_count,
                },
            )
            inserted_count += res.rowcount

    print(f"[{datetime.now()}] 同步完成！新增事实记录: {inserted_count} 条")
