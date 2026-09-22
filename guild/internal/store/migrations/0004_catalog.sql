-- 物品分类树。id+parent 做主键:同一个子类可能挂在多个大类下
CREATE TABLE IF NOT EXISTS item_category (
    id       TEXT NOT NULL,
    parent   TEXT NOT NULL DEFAULT '',
    label_zh TEXT NOT NULL DEFAULT '',
    label_en TEXT NOT NULL DEFAULT '',
    sort     INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (id, parent)
);

-- 图标是二进制且永不变化,天生适合当 BLOB 存。
-- 不存的话,翻一页分类就是上百个并发请求直打 render.albiononline.com,
-- 会被对面限流,列表里一半图标是空的
CREATE TABLE IF NOT EXISTS item_icon (
    item_id    TEXT PRIMARY KEY,
    png        BYTEA,
    fetched_at TIMESTAMPTZ NOT NULL,
    missing    BOOLEAN NOT NULL DEFAULT FALSE
);

-- 目录同步时间之类的小状态
CREATE TABLE IF NOT EXISTS catalog_meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
