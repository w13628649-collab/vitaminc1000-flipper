-- 精炼 / 转化配方。来自 ao-bin-dumps 的原始 items.json,和物品目录同一次下载、
-- 同一次同步,整份替换(上游删掉的配方不该永久留在库里)。
--
-- 不按规则生成:转化银币不规整(16,500、51,975、155,925……),而且随版本改过
-- (Wild Blood 改过一次),数据文件本身就是权威来源。
-- 库里没有时服务端用编进二进制的快照兜底(refine/data/recipes_*.json)。
CREATE TABLE IF NOT EXISTS recipe (
    -- 产物的市场 id(T8_ORE_LEVEL4@4),和挂单、AODP 一致;dump 里叫 T8_ORE_LEVEL4
    item_id           TEXT             NOT NULL,
    -- dump 里是这个物品的第几条 craftingrequirements:心配方、两个方向的转化各占一条
    variant           SMALLINT         NOT NULL,
    kind              TEXT             NOT NULL CHECK (kind IN ('refine', 'transmute')),
    -- 转化的基础银币;游戏实付还要乘 k、加主人费用,那是计算层的事
    silver            BIGINT           NOT NULL DEFAULT 0,
    focus             INTEGER          NOT NULL DEFAULT 0,
    amount            INTEGER          NOT NULL DEFAULT 1,
    -- 产物的物品价值,站费和转化主人费用按它算
    item_value        DOUBLE PRECISION NOT NULL DEFAULT 0,
    unlocked_to_craft BOOLEAN          NOT NULL DEFAULT FALSE,
    -- [{"item": "...", "count": 5, "max_return": 0}],max_return 缺省 = dump 里没写
    inputs            JSONB            NOT NULL,
    synced_at         TIMESTAMPTZ      NOT NULL,
    PRIMARY KEY (item_id, variant)
);
