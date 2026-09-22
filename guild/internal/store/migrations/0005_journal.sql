-- 交易记账。这张表是整个项目最有价值的一张:
-- 所有估算模型里最虚的那个数是 absorb_ratio(敢吃日成交量的多大比例),
-- 现在是拍脑袋的 0.20。只有真实成交记录能校准它。
CREATE TABLE IF NOT EXISTS trade (
    id          BIGSERIAL PRIMARY KEY,
    owner       TEXT        NOT NULL,          -- 谁记的,公会里各记各的
    item_id     TEXT        NOT NULL,
    quality     SMALLINT    NOT NULL DEFAULT 1,

    kind        TEXT        NOT NULL,          -- 'flip' 同城 | 'arb' 跨城
    buy_city    TEXT        NOT NULL,
    sell_city   TEXT        NOT NULL,
    mode        TEXT        NOT NULL,          -- taker-taker / maker-maker / …

    -- 下单时的计划
    planned_qty   BIGINT    NOT NULL,
    planned_buy   BIGINT    NOT NULL,
    planned_sell  BIGINT    NOT NULL,
    -- 下单时模型预估的日均成交量,校准 absorb_ratio 要拿它当分母
    market_daily_qty DOUBLE PRECISION NOT NULL DEFAULT 0,

    -- 实际成交。没填就是还没收口
    filled_buy_qty   BIGINT,
    filled_buy_price BIGINT,
    filled_sell_qty  BIGINT,
    filled_sell_price BIGINT,

    -- 实际到手的净利(已扣税费)。收口时算好存下来,
    -- 免得以后改了税率把历史账目一起改掉
    realized_profit BIGINT,

    status      TEXT        NOT NULL DEFAULT 'open',  -- open | filled | partial | abandoned
    note        TEXT        NOT NULL DEFAULT '',
    opened_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    closed_at   TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_trade_owner ON trade (owner, opened_at DESC);
CREATE INDEX IF NOT EXISTS idx_trade_item  ON trade (item_id, quality, status);
