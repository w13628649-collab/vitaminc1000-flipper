-- 原始样本:还没解过字段布局的消息原样(每个参数带 Go 类型标签),之后照着写解码器。
-- 只收客户端白名单里标了 Sample 的几条(op 197 响应、专精事件 150/152/153)。
-- 每个 (kind, code) 只留最近 200 条:样本是给人看布局的,不是数据。
CREATE TABLE IF NOT EXISTS capture_sample (
    id             BIGSERIAL   PRIMARY KEY,
    kind           TEXT        NOT NULL,              -- op_response | event
    code           INTEGER     NOT NULL,
    params         JSONB       NOT NULL,              -- [{"k":17,"t":"[]int32","v":[…]}, …]
    observed_at    TIMESTAMPTZ NOT NULL,              -- 已纠偏到服务端时钟
    client_version TEXT        NOT NULL DEFAULT '',
    reporter       TEXT        NOT NULL DEFAULT '',
    received_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_sample_code ON capture_sample (kind, code, observed_at DESC);
