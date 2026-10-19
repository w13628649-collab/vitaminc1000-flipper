-- 精炼专精(2026-10-19):抓包读到的命运之轮精炼节点等级(事件 150),精炼页拿它自动填专精 FCE
-- 和「解锁到 T 几」。客户端只在等级变了、或换了角色时报,一行就是一次变化。

CREATE TABLE IF NOT EXISTS my_spec (
    character   TEXT        NOT NULL,
    -- 线 → T4~T8 节点等级:{"ORE":[76,100,91,1,1], ...}。0 = 没解锁,100 = 满级
    levels      JSONB       NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (character, observed_at)
);
