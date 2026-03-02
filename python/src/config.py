import yaml
import os
import re

# 定位到 GoKit 项目根目录 (当前文件是在 etl/src/ 下)
BASE_DIR = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
CONFIG_PATH = os.path.join(BASE_DIR, "configs", "config.yaml")

def get_sqlalchemy_uri() -> str:
    """读取 config.yaml 并将 Go DSN 转换为 SQLAlchemy URI"""
    if not os.path.exists(CONFIG_PATH):
        raise FileNotFoundError(f"找不到配置文件: {CONFIG_PATH}")

    with open(CONFIG_PATH, 'r', encoding='utf-8') as f:
        config = yaml.safe_load(f)

    # 从 GoKit 的配置结构中获取 dsn
    dsn = config.get("database", {}).get("dsn", "")
    if not dsn:
        raise ValueError("config.yaml 中未配置 database.dsn")

    # 解析 Go 的 MySQL DSN: root:root@tcp(127.0.0.1:3306)/my_db?charset=utf8mb4...
    pattern = r"^(?P<user>[^:]+):(?P<password>[^@]+)@tcp\((?P<host_port>[^)]+)\)/(?P<db>[^\?]+)(?:\?(?P<params>.*))?$"
    match = re.match(pattern, dsn)
    if not match:
        raise ValueError(f"无法解析的 DSN 格式: {dsn}")

    d = match.groupdict()

    # 构建 SQLAlchemy 兼容的 URI (忽略 Go 特有的 parseTime=True 等参数，强制指定 pymysql 需要的 charset)
    uri = f"mysql+pymysql://{d['user']}:{d['password']}@{d['host_port']}/{d['db']}?charset=utf8mb4"
    return uri
