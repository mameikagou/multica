-- Keep usage reports current without weakening the hourly rollup's five-minute
-- safety lag. Historical buckets continue to come from task_usage_hourly; the
-- bucket that contains the lag boundary (and every newer bucket) is rebuilt
-- from raw task_usage rows at read time.
--
-- The boundary is rounded down to an hour. At most 65 minutes of raw rows are
-- scanned in steady state: five minutes of intentional lag plus the remainder
-- of the boundary hour. Splitting on a whole bucket is what prevents a row
-- already present in the rollup from being counted again in the live tail.
CREATE VIEW task_usage_hourly_live AS
WITH boundary AS (
    SELECT task_usage_hour_bucket(now() - INTERVAL '5 minutes') AS cutoff
),
rolled AS (
    SELECT
        h.bucket_hour,
        h.workspace_id,
        h.runtime_id,
        h.agent_id,
        h.project_id,
        h.provider,
        h.model,
        h.input_tokens,
        h.output_tokens,
        h.cache_read_tokens,
        h.cache_write_tokens,
        h.cost_usd_ticks,
        COALESCE(h.uncosted_input_tokens, h.input_tokens)::bigint AS uncosted_input_tokens,
        COALESCE(h.uncosted_output_tokens, h.output_tokens)::bigint AS uncosted_output_tokens,
        COALESCE(h.uncosted_cache_read_tokens, h.cache_read_tokens)::bigint AS uncosted_cache_read_tokens,
        COALESCE(h.uncosted_cache_write_tokens, h.cache_write_tokens)::bigint AS uncosted_cache_write_tokens,
        h.task_count,
        h.event_count
    FROM task_usage_hourly h
    CROSS JOIN boundary b
    WHERE h.bucket_hour < b.cutoff
),
live_tail AS (
    SELECT
        task_usage_hour_bucket(tu.created_at) AS bucket_hour,
        a.workspace_id,
        atq.runtime_id,
        atq.agent_id,
        i.project_id,
        tu.provider,
        tu.model,
        SUM(tu.input_tokens)::bigint AS input_tokens,
        SUM(tu.output_tokens)::bigint AS output_tokens,
        SUM(tu.cache_read_tokens)::bigint AS cache_read_tokens,
        SUM(tu.cache_write_tokens)::bigint AS cache_write_tokens,
        COALESCE(SUM(tu.cost_usd_ticks), 0)::bigint AS cost_usd_ticks,
        COALESCE(SUM(tu.input_tokens) FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_input_tokens,
        COALESCE(SUM(tu.output_tokens) FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_output_tokens,
        COALESCE(SUM(tu.cache_read_tokens) FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_cache_read_tokens,
        COALESCE(SUM(tu.cache_write_tokens) FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_cache_write_tokens,
        COUNT(DISTINCT tu.task_id)::bigint AS task_count,
        COUNT(*)::bigint AS event_count
    FROM task_usage tu
    JOIN agent_task_queue atq ON atq.id = tu.task_id
    JOIN agent a ON a.id = atq.agent_id
    LEFT JOIN issue i ON i.id = atq.issue_id
    CROSS JOIN boundary b
    WHERE atq.runtime_id IS NOT NULL
      AND tu.created_at >= b.cutoff
    GROUP BY 1, 2, 3, 4, 5, 6, 7
)
SELECT * FROM rolled
UNION ALL
SELECT * FROM live_tail;

COMMENT ON VIEW task_usage_hourly_live IS
    'Hourly usage with a raw live tail for the rollup safety-lag window. Use for reports; keep task_usage_hourly as the materialized storage table.';
