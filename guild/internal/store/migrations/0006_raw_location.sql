-- 抓包报上来的原始地点 id(0007 这种)。location_id 入库前已经收敛成城市名,
-- 收敛规则将来改了,要靠这一列重算;只存结果的话老数据和新数据会分裂成两个 key
ALTER TABLE market_order_live  ADD COLUMN IF NOT EXISTS raw_location TEXT NOT NULL DEFAULT '';
ALTER TABLE market_order_event ADD COLUMN IF NOT EXISTS raw_location TEXT NOT NULL DEFAULT '';
