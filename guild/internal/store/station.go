package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"albion-guild/internal/model"
	"albion-guild/internal/world"
)

// StationFee 是一个建筑最新的一次观测。
type StationFee struct {
	model.StationObs
	// City 是 world.ClusterCity 翻出来的城市名;不是城区的保持原始区 id
	City        string `json:"city"`
	BuildingKey string `json:"building_key"`
	Reporter    string `json:"reporter"`
}

// SaveStations 存一批建筑观测(纯 append)。ObservedAt 由调用方先纠偏到服务端时钟。
func (s *Store) SaveStations(ctx context.Context, reporter string, obs []model.StationObs) error {
	if len(obs) == 0 {
		return nil
	}
	rows := make([][]any, 0, len(obs))
	for _, o := range obs {
		city, _ := world.ClusterCity(o.ZoneID)
		var access any
		if o.Access != nil {
			access = int16(*o.Access)
		}
		rows = append(rows, []any{
			o.ZoneID, city, o.BuildingKey(), o.Index, o.BuildingType, nilIfEmpty(o.Pos),
			o.Durability, o.Nutrition, o.Silver, o.Owner,
			o.AssociateFeeRaw, o.AssociateFee, o.PublicFeeRaw, o.PublicFee,
			access, o.CanUse, o.Usable,
			jsonOrNil(o.Key23), jsonOrNil(o.Key31), o.ObservedAt, reporter,
		})
	}
	_, err := s.pool.CopyFrom(ctx, pgx.Identifier{"station_obs"},
		[]string{"zone_id", "city", "building_key", "idx", "building_type", "pos",
			"durability", "nutrition", "silver", "owner",
			"associate_fee_raw", "associate_fee", "public_fee_raw", "public_fee",
			"access", "can_use", "usable",
			"key23", "key31", "observed_at", "reporter"},
		pgx.CopyFromRows(rows))
	return err
}

// StationFees 返回 since 之后观测过的每个 (区, 建筑) 最新一条,按城市、区、建筑类型排。
func (s *Store) StationFees(ctx context.Context, since time.Time) ([]StationFee, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT zone_id, city, building_key, idx, building_type, pos,
			durability, nutrition, silver, owner,
			associate_fee_raw, associate_fee, public_fee_raw, public_fee,
			access, can_use, usable,
			key23, key31, observed_at, reporter
		FROM (
			SELECT DISTINCT ON (zone_id, building_key) *
			FROM station_obs
			WHERE observed_at > $1
			ORDER BY zone_id, building_key, observed_at DESC, id DESC
		) latest
		ORDER BY city, zone_id, building_type, building_key`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []StationFee
	for rows.Next() {
		var f StationFee
		var idx, btype int32
		var access *int16
		var key23, key31 []byte
		if err := rows.Scan(&f.ZoneID, &f.City, &f.BuildingKey, &idx, &btype, &f.Pos,
			&f.Durability, &f.Nutrition, &f.Silver, &f.Owner,
			&f.AssociateFeeRaw, &f.AssociateFee, &f.PublicFeeRaw, &f.PublicFee,
			&access, &f.CanUse, &f.Usable,
			&key23, &key31, &f.ObservedAt, &f.Reporter); err != nil {
			return nil, err
		}
		if access != nil {
			f.Access = ptrInt32(int32(*access))
		}
		f.Index, f.BuildingType = int(idx), btype
		f.Key23, f.Key31 = key23, key31
		out = append(out, f)
	}
	return out, rows.Err()
}

func ptrInt32(v int32) *int32 { return &v }

func nilIfEmpty(p []float64) any {
	if len(p) == 0 {
		return nil
	}
	return p
}

// jsonOrNil:空的 RawMessage 写 SQL NULL,不写空串(jsonb 不收空串)
func jsonOrNil(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	return raw
}
