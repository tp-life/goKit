from sqlalchemy import create_engine

from .config import get_sqlalchemy_uri

# 创建全局唯一的 SQLAlchemy Engine
engine = create_engine(
    get_sqlalchemy_uri(),
    pool_recycle=3600,  # 一小时回收连接，防止 MySQL 主动断开
    pool_size=5,
    max_overflow=10,
)


def get_db_connection():
    """提供一个带事务的数据库连接上下文"""
    return engine.begin()
