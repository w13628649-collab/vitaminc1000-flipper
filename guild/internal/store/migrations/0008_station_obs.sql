-- 区域地图(op 197 GetClusterMapInfo)里的建筑和制造站费率,一行是一次观测里的一个建筑。
-- 纯 append:费率是房主设的服务器状态,历史本身有用(谁什么时候改过价);
-- "现在多少"由 StationFees 按 (区, 建筑) 取最新一条。
--
-- 这块数据没有 AODP 兜底(它的地图数据只写不读),错了没有第二路能发现,
-- 所以原值和归一化值都存,含义未知的 23/31 也原样存,将来口径改了能重算。
CREATE TABLE IF NOT EXISTS station_obs (
    id                BIGSERIAL        PRIMARY KEY,
    zone_id           TEXT             NOT NULL,              -- 197 请求的 key0,纯数字
    city              TEXT             NOT NULL,              -- world.ClusterCity;不是城区的存原始 id
    building_key      TEXT             NOT NULL,              -- model.StationObs.BuildingKey:类型 + 坐标
    idx               INTEGER          NOT NULL,              -- 在那次响应数组里的下标
    building_type     INTEGER          NOT NULL,              -- key17
    pos               DOUBLE PRECISION[],                     -- key18,只当连接键
    durability        BIGINT,                                 -- key20,原值
    nutrition         BIGINT,                                 -- key22,原值
    silver            BIGINT,                                 -- key24,原值
    owner             TEXT             NOT NULL DEFAULT '',   -- key25
    -- key33 = Associate、key34 = Public 只有社区一个来源,待游戏内截图确认
    associate_fee_raw BIGINT,
    associate_fee     DOUBLE PRECISION,                       -- 归一化:raw > 1000 按 /10000
    public_fee_raw    BIGINT,
    public_fee        DOUBLE PRECISION,
    key23             JSONB,                                  -- 含义未知,带类型标签原样
    key31             JSONB,
    observed_at       TIMESTAMPTZ      NOT NULL,              -- 已纠偏到服务端时钟
    reporter          TEXT             NOT NULL DEFAULT '',
    received_at       TIMESTAMPTZ      NOT NULL DEFAULT now()
);
-- StationFees:每个 (区, 建筑) 最新一条
CREATE INDEX IF NOT EXISTS idx_station_latest ON station_obs (zone_id, building_key, observed_at DESC);
CREATE INDEX IF NOT EXISTS idx_station_time   ON station_obs (observed_at);
