package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"albion-guild/internal/model"
)

// SamplesPerCode 是每个 (kind, code) 在库里留多少条原始样本。
const SamplesPerCode = 200

// SampleRow 是导出的一条原始样本。
type SampleRow struct {
	ID            int64           `json:"id"`
	Kind          string          `json:"kind"`
	Code          int             `json:"code"`
	Params        json.RawMessage `json:"params"`
	ObservedAt    time.Time       `json:"observed_at"`
	ClientVersion string          `json:"client_version"`
	Reporter      string          `json:"reporter"`
	ReceivedAt    time.Time       `json:"received_at"`
}

// SaveSamples 存一批原始样本,然后把涉及到的 (kind, code) 修剪到最近 SamplesPerCode 条。
// 一个事务:修剪失败不留下超额的行。
func (s *Store) SaveSamples(ctx context.Context, reporter string, samples []model.CaptureSample) error {
	if len(samples) == 0 {
		return nil
	}
	rows := make([][]any, 0, len(samples))
	kinds := make([]string, 0, len(samples))
	codes := make([]int32, 0, len(samples))
	for _, sm := range samples {
		if !json.Valid(sm.Params) {
			return fmt.Errorf("样本 %s/%d 的 params 不是合法 JSON", sm.Kind, sm.Code)
		}
		rows = append(rows, []any{sm.Kind, int32(sm.Code), []byte(sm.Params), sm.ObservedAt,
			sm.ClientVersion, reporter})
		kinds = append(kinds, sm.Kind)
		codes = append(codes, int32(sm.Code))
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"capture_sample"},
		[]string{"kind", "code", "params", "observed_at", "client_version", "reporter"},
		pgx.CopyFromRows(rows)); err != nil {
		return fmt.Errorf("写入样本: %w", err)
	}
	// 只修剪这批涉及到的 (kind, code)。unnest 成对展开,不是两个数组的笛卡尔积
	if _, err := tx.Exec(ctx, `
		DELETE FROM capture_sample c USING (
			SELECT id FROM (
				SELECT id, row_number() OVER (
					PARTITION BY kind, code ORDER BY observed_at DESC, id DESC) AS rn
				FROM capture_sample
				WHERE (kind, code) IN (SELECT * FROM unnest($1::text[], $2::int[]))
			) ranked WHERE rn > $3
		) old
		WHERE c.id = old.id`, kinds, codes, SamplesPerCode); err != nil {
		return fmt.Errorf("修剪样本: %w", err)
	}
	return tx.Commit(ctx)
}

// Samples 按观测时间倒序导出原始样本。code < 0 表示不筛码,kind 为空表示不筛方向。
func (s *Store) Samples(ctx context.Context, kind string, code, limit int) ([]SampleRow, error) {
	if limit <= 0 || limit > SamplesPerCode {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, kind, code, params, observed_at, client_version, reporter, received_at
		FROM capture_sample
		WHERE ($1 = '' OR kind = $1) AND ($2 < 0 OR code = $2)
		ORDER BY observed_at DESC, id DESC
		LIMIT $3`, kind, code, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SampleRow
	for rows.Next() {
		var r SampleRow
		var code32 int32
		var params []byte
		if err := rows.Scan(&r.ID, &r.Kind, &code32, &params, &r.ObservedAt,
			&r.ClientVersion, &r.Reporter, &r.ReceivedAt); err != nil {
			return nil, err
		}
		r.Code, r.Params = int(code32), params
		out = append(out, r)
	}
	return out, rows.Err()
}
