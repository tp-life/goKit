#!/bin/bash
# ========================================================
# GoKit 量化爬虫 - 首次部署与基线初始化脚本
# ========================================================

# 1. 环境变量与目录设定
export LC_ALL=zh_CN.UTF-8
export LANG=zh_CN.UTF-8
BASE_DIR=$(cd "$(dirname "$0")" && pwd)
TASK_SCRIPT="$BASE_DIR/auto_task.sh"

cd "$BASE_DIR"
echo "==== 1. 开始编译 Go 爬虫引擎 ===="
mkdir -p bin logs

# 编译 Go 项目并输出到 bin 目录
go build -o bin/crawler_job cmd/job/main.go
if [ $? -ne 0 ]; then
    echo "❌ 编译失败，请检查 Go 环境和代码！"
    exit 1
fi
echo "✅ 编译成功: 产物位于 bin/crawler_job"

echo ""
echo "==== 2. 首次执行：基线数据全量拉取 ===="
echo "系统需要一份全量历史数据作为分析的底座。"
# 交互式询问，带 15 秒超时防阻塞
read -t 15 -p "请输入最近一次的财报期用于初始化 (如 2023-09-30，直接回车或等待15秒默认使用): " BASE_DATE
BASE_DATE=${BASE_DATE:-"2025-01-01"}

echo "🚀 正在拉取 $BASE_DATE 的全市场持仓数据，得益于批量并发，这大约需要十几秒..."
./bin/crawler_job -mode=full -date="$BASE_DATE"

echo ""
echo "==== 3. 配置自动化守护进程 (Crontab) ===="
chmod +x "$TASK_SCRIPT"

# 设定为: 每天晚上 23:00 自动执行 (财报通常在盘后至晚间集中披露)
CRON_RULE="0 23 * * * $TASK_SCRIPT"

if crontab -l 2>/dev/null | grep -q "$TASK_SCRIPT"; then
    echo "✅ 检测到任务已存在于 crontab 中，已更新配置。"
else
    (crontab -l 2>/dev/null; echo "$CRON_RULE") | crontab -
    echo "🎉 部署成功！已将自动化脚本注入系统守护进程。"
fi

echo "日志输出路径: $BASE_DIR/logs/crawler_cron.log"
