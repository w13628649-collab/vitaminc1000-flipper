-- 个人账本(2026-10-19):抓包自动记下的自己的成交、银币余额、专注余额。
-- 和 0005 的 trade 表不是一回事:那张是手填的"计划 vs 实际",这几张是抓包看到的事实。

CREATE TABLE IF NOT EXISTS my_trade (
    id           BIGSERIAL   PRIMARY KEY,
    -- 自然键:instant:<订单号>:<毫秒> / mail:<邮件 id>。同一封邮件每次打开都会再传一遍,靠它去重
    key          TEXT        NOT NULL UNIQUE,
    kind         TEXT        NOT NULL,              -- instant_buy / instant_sell / order_sold / ...
    character    TEXT        NOT NULL DEFAULT '',
    item_id      TEXT        NOT NULL,
    quality      SMALLINT    NOT NULL DEFAULT 0,    -- 0 = 不知道(市场邮件不带品质)
    amount       INTEGER     NOT NULL,              -- 实际成交件数,到期类可以是 0
    order_amount INTEGER     NOT NULL DEFAULT 0,    -- 挂单总件数,到期类才有
    unit_price   BIGINT      NOT NULL,              -- 银币,税前
    total        BIGINT      NOT NULL,              -- 银币,税前
    -- 税和创建费在入库时按当时的税率算好存下来,以后改税率不改历史账目
    tax          BIGINT      NOT NULL DEFAULT 0,    -- 销售税:卖出才有
    fee          BIGINT      NOT NULL DEFAULT 0,    -- 创建费:挂单才有,不管成没成交
    city         TEXT        NOT NULL DEFAULT '',   -- 收敛后的地点(world.City)
    raw_location TEXT        NOT NULL DEFAULT '',   -- 包里的原始地点 id
    order_id     BIGINT,
    mail_id      BIGINT,
    observed_at  TIMESTAMPTZ NOT NULL,              -- 成交时刻
    received_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    reporter     TEXT        NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_my_trade_time ON my_trade (observed_at DESC);
CREATE INDEX IF NOT EXISTS idx_my_trade_item ON my_trade (item_id, observed_at DESC);

CREATE TABLE IF NOT EXISTS my_wallet (
    character   TEXT        NOT NULL,
    silver      BIGINT      NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (character, observed_at)
);

CREATE TABLE IF NOT EXISTS my_focus (
    character   TEXT             NOT NULL,
    focus       DOUBLE PRECISION NOT NULL,
    observed_at TIMESTAMPTZ      NOT NULL,
    PRIMARY KEY (character, observed_at)
);
