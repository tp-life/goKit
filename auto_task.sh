#!/bin/bash
# ========================================================
# GoKit 量化爬虫 - 每日自动化调度中枢
# ========================================================

# 1. 统一中文环境，防止 Go 程序解析含中文的时间或数据时出现乱码
export LC_ALL=zh_CN.UTF-8
export LANG=zh_CN.UTF-8

# 2. 定位目录结构
BASE_DIR=$(cd "$(dirname "$0")" && pwd)
LOG_FILE="$BASE_DIR/logs/crawler_cron.log"
JOB_BIN="$BASE_DIR/bin/crawler_job"

cd "$BASE_DIR"

echo "========================================" >> "$LOG_FILE"
echo "[$(date '+%Y-%m-%d %H:%M:%S')] 自动调度任务开始" >> "$LOG_FILE"

# ========================================================
# 核心任务 1：每日增量拉取 (365天每天都跑)
# 逻辑：Go 引擎会自动获取东方财富后台过去 24 小时内有 `UPDATE_DATE` 变动的股票。
# 优势：速度极快，网络开销极低。
# ========================================================
echo "--> [每日任务] 开始增量同步最新披露的持仓数据" >> "$LOG_FILE"
$JOB_BIN -mode=incremental >> "$LOG_FILE" 2>&1

# ========================================================
# 核心任务 2：财报季全量兜底 (仅在特定月份触发)
# 逻辑：虽然有每日增量，但为了防止网络波动漏抓，我们在财报披露大月，强制进行一次全市场洗盘。
# 优势：Go 引擎底层使用了 `OnConflict{DoNothing: true}`，即便数据已存在也不会报错，绝对保证数据完整性。
# ========================================================
CURRENT_MONTH=$(date +%m)
CURRENT_YEAR=$(date +%Y)
LAST_YEAR=$((CURRENT_YEAR - 1))

if [ "$CURRENT_MONTH" == "04" ]; then
    # 4月：年报与一季报集中披露期
    echo "--> [财报季-4月兜底] 拉取上年年报 & 本年一季报" >> "$LOG_FILE"
    $JOB_BIN -mode=full -date="${LAST_YEAR}-12-31" >> "$LOG_FILE" 2>&1
    $JOB_BIN -mode=full -date="${CURRENT_YEAR}-03-31" >> "$LOG_FILE" 2>&1

elif [ "$CURRENT_MONTH" == "08" ]; then
    # 8月：中报集中披露期
    echo "--> [财报季-8月兜底] 拉取本年中报" >> "$LOG_FILE"
    $JOB_BIN -mode=full -date="${CURRENT_YEAR}-06-30" >> "$LOG_FILE" 2>&1

elif [ "$CURRENT_MONTH" == "10" ]; then
    # 10月：三季报集中披露期
    echo "--> [财报季-10月兜底] 拉取本年三季报" >> "$LOG_FILE"
    $JOB_BIN -mode=full -date="${CURRENT_YEAR}-09-30" >> "$LOG_FILE" 2>&1
else
    echo "--> 非财报集中披露月 ($CURRENT_MONTH 月)，已跳过全量兜底" >> "$LOG_FILE"
fi

echo "[$(date '+%Y-%m-%d %H:%M:%S')] 自动调度任务结束" >> "$LOG_FILE"
