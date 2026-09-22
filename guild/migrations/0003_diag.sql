-- 客户端诊断上报。200 人规模下,出了问题没法一个个远程看屏幕,
-- 客户端把警告和错误传回来才有的查。
CREATE TABLE IF NOT EXISTS client_diag (
    id          BIGSERIAL   PRIMARY KEY,
    reported_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    client_id   TEXT        NOT NULL DEFAULT '',  -- 设备标识,首次启动生成后持久化
    character   TEXT        NOT NULL DEFAULT '',  -- 角色名,知道了才填
    version     TEXT        NOT NULL DEFAULT '',
    os          TEXT        NOT NULL DEFAULT '',
    level       TEXT        NOT NULL,             -- info | warn | error
    message     TEXT        NOT NULL,
    attrs       JSONB
);
CREATE INDEX IF NOT EXISTS idx_diag_recent ON client_diag (reported_at DESC);
CREATE INDEX IF NOT EXISTS idx_diag_client ON client_diag (client_id, reported_at DESC);
