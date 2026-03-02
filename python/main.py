import argparse
from datetime import datetime

from src.jobs.holdings import sync_quarterly_holdings
from src.jobs.quotes import sync_daily_quotes


def main():
    parser = argparse.ArgumentParser(description="GoKit 量化 ETL 工具链")
    subparsers = parser.add_subparsers(dest="command", help="可用的命令", required=True)

    # 1. 注册 holdings 命令
    holdings_parser = subparsers.add_parser(
        "holdings", help="同步特定财报期的机构持仓数据"
    )
    holdings_parser.add_argument(
        "--date", required=True, help="财报日期 (格式: YYYYMMDD, 如 20230930)"
    )

    # 2. 注册 quotes 命令
    quotes_parser = subparsers.add_parser("quotes", help="同步国家队持仓股票的日线行情")
    quotes_parser.add_argument("--start", help="开始日期 (默认今天, 格式: YYYYMMDD)")
    quotes_parser.add_argument("--end", help="结束日期 (默认今天, 格式: YYYYMMDD)")

    args = parser.parse_args()

    if args.command == "holdings":
        sync_quarterly_holdings(args.date)

    elif args.command == "quotes":
        today = datetime.now().strftime("%Y%m%d")
        start_date = args.start if args.start else today
        end_date = args.end if args.end else today
        sync_daily_quotes(start_date, end_date)


if __name__ == "__main__":
    main()
