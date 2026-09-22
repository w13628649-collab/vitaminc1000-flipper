-- 物品目录:从 ao-bin-dumps 同步过来,客户端和界面都要用中文名
CREATE TABLE IF NOT EXISTS item (
    item_id     TEXT PRIMARY KEY,
    name_zh     TEXT NOT NULL DEFAULT '',
    name_en     TEXT NOT NULL DEFAULT '',
    category    TEXT NOT NULL DEFAULT '',
    subcategory TEXT NOT NULL DEFAULT '',
    family      TEXT NOT NULL DEFAULT '',
    tier        SMALLINT NOT NULL DEFAULT 0,
    enchant     SMALLINT NOT NULL DEFAULT 0,
    max_quality SMALLINT NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS idx_item_browse ON item (category, subcategory, tier, enchant);

-- 当前活跃挂单。普通表,可 upsert,"现在什么价"走这张
CREATE TABLE IF NOT EXISTS market_order_live (
    order_id    BIGINT      PRIMARY KEY,
    item_id     TEXT        NOT NULL,
    location_id TEXT        NOT NULL,
    quality     SMALLINT    NOT NULL,
    enchant     SMALLINT    NOT NULL DEFAULT 0,
    side        SMALLINT    NOT NULL,          -- 0=卖单 offer, 1=买单 request
    unit_price  BIGINT      NOT NULL,          -- 银币,已从协议的 ×10000 还原
    amount      INTEGER     NOT NULL,
    expires_at  TIMESTAMPTZ,
    first_seen  TIMESTAMPTZ NOT NULL,
    last_seen   TIMESTAMPTZ NOT NULL,
    reporter    TEXT        NOT NULL DEFAULT ''
);

-- 订单簿查询的主力索引:按物品+城市+品质+方向找,价格有序
CREATE INDEX IF NOT EXISTS idx_live_book
    ON market_order_live (item_id, location_id, quality, side, unit_price)
    INCLUDE (amount, last_seen);
-- 清理很久没再看到的挂单
CREATE INDEX IF NOT EXISTS idx_live_stale ON market_order_live (last_seen);

-- 挂单变更流。hypertable,纯 append,"当时什么价"走这张
CREATE TABLE IF NOT EXISTS market_order_event (
    order_id    BIGINT      NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    item_id     TEXT        NOT NULL,
    location_id TEXT        NOT NULL,
    quality     SMALLINT    NOT NULL,
    enchant     SMALLINT    NOT NULL DEFAULT 0,
    side        SMALLINT    NOT NULL,
    unit_price  BIGINT      NOT NULL,
    amount      INTEGER     NOT NULL,
    reporter    TEXT        NOT NULL DEFAULT ''
);
SELECT create_hypertable('market_order_event', 'observed_at',
                         chunk_time_interval => INTERVAL '1 day',
                         if_not_exists => TRUE);
CREATE INDEX IF NOT EXISTS idx_evt_lookup
    ON market_order_event (item_id, location_id, quality, side, observed_at DESC);

-- 成交历史。AODP 和抓包字段集一致,可同表,source 进主键
CREATE TABLE IF NOT EXISTS market_history (
    item_id      TEXT        NOT NULL,
    location_id  TEXT        NOT NULL,
    quality      SMALLINT    NOT NULL,
    timescale    SMALLINT    NOT NULL,        -- 0 时 / 1 日 / 2 周
    bucket       TIMESTAMPTZ NOT NULL,
    item_count   BIGINT      NOT NULL,
    silver_total BIGINT,                      -- 抓包有,AODP 没有
    avg_price    BIGINT      NOT NULL,
    source       TEXT        NOT NULL,        -- 'aodp' | 'capture'
    observed_at  TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (item_id, location_id, quality, timescale, bucket, source)
);
CREATE INDEX IF NOT EXISTS idx_hist_lookup
    ON market_history (item_id, location_id, quality, timescale, bucket DESC);
