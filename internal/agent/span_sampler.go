package agent

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
)

// SpanSampler queries recent tool call samples from the spans table.
// Implements ToolSpanSampler using direct SQL to avoid coupling to TracingStore.
type SpanSampler struct {
	db *sql.DB
}

// NewSpanSampler creates a span sampler from a database connection.
func NewSpanSampler(db *sql.DB) *SpanSampler {
	return &SpanSampler{db: db}
}

const spanSampleQuery = `
SELECT
    COALESCE(LEFT(s.input_preview, 200), '') AS input,
    COALESCE(LEFT(s.output_preview, 200), '') AS output
FROM spans s
JOIN traces t ON t.id = s.trace_id
WHERE t.agent_id = $1
  AND s.name = $2
  AND s.status = 'completed'
ORDER BY s.start_time DESC
LIMIT $3`

// SampleToolSpans returns recent successful tool call input/output samples.
func (ss *SpanSampler) SampleToolSpans(ctx context.Context, agentID uuid.UUID, toolName string, limit int) ([]ToolSpanSample, error) {
	if ss.db == nil {
		return nil, fmt.Errorf("no database connection")
	}

	rows, err := ss.db.QueryContext(ctx, spanSampleQuery, agentID, toolName, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var samples []ToolSpanSample
	for rows.Next() {
		var s ToolSpanSample
		if err := rows.Scan(&s.Input, &s.Output); err != nil {
			continue
		}
		samples = append(samples, s)
	}
	return samples, rows.Err()
}
