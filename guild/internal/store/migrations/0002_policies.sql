-- 明细 14 天后压缩,180 天后丢弃(那时聚合已经留下来了)
ALTER TABLE market_order_event SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'item_id, location_id, quality, side'
);
SELECT add_compression_policy('market_order_event', INTERVAL '14 days', if_not_exists => TRUE);
SELECT add_retention_policy('market_order_event', INTERVAL '180 days', if_not_exists => TRUE);
