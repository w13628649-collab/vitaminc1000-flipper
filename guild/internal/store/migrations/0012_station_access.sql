-- 工作站能不能用(2026-10-19):op 197 的 key31 是房主设的访问权限预设
-- AccessRights.Preset(Closed 0 / Private 1 / Public 2 / Custom 3),key30 / key32 是对抓包的
-- 这个角色能不能用。挑最便宜的站时排除用不了的——以前 Closed 的站也会被当成最便宜那台。
-- 老行没有这三列(NULL),照旧当能用。key31 解开之后不再原样存。
ALTER TABLE station_obs ADD COLUMN IF NOT EXISTS access  SMALLINT;
ALTER TABLE station_obs ADD COLUMN IF NOT EXISTS can_use BOOLEAN;
ALTER TABLE station_obs ADD COLUMN IF NOT EXISTS usable  BOOLEAN;
