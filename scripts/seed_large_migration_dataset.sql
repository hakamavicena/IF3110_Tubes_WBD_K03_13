\set ON_ERROR_STOP on
\timing on

\set initial_rows 1000000
\set maximum_rows 2000000

DO $guard$
BEGIN
    IF current_database() <> 'target2_db' THEN
        RAISE EXCEPTION
            'Refusing to seed large migration data in database "%"; connect to target2_db',
            current_database();
    END IF;
END;
$guard$;

SELECT set_config('large_dataset.initial_rows', :'initial_rows', false);
SELECT set_config('large_dataset.maximum_rows', :'maximum_rows', false);

DO $limits$
BEGIN
    IF current_setting('large_dataset.initial_rows')::BIGINT < 1 OR
       current_setting('large_dataset.maximum_rows')::BIGINT <
           current_setting('large_dataset.initial_rows')::BIGINT THEN
        RAISE EXCEPTION
            'Dataset row limits are invalid: initial_rows must be positive and maximum_rows >= initial_rows';
    END IF;
END;
$limits$;

CREATE TABLE IF NOT EXISTS large_migration_dataset (
    id BIGINT PRIMARY KEY,
    batch_value BIGINT NOT NULL DEFAULT 0,
    payload TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS large_dataset_benchmark_runs (
    run_id BIGSERIAL PRIMARY KEY,
    measured_at TIMESTAMPTZ NOT NULL,
    rows_affected BIGINT NOT NULL,
    duration_ms BIGINT NOT NULL
);

-- This script owns this table and resets only its test data when rerun.
TRUNCATE TABLE large_migration_dataset;

INSERT INTO large_migration_dataset (id, batch_value, payload, updated_at)
SELECT id,
       0,
       repeat(md5('large-dataset-seed-' || id::text), 8),
       transaction_timestamp()
FROM generate_series(
    1::BIGINT,
    current_setting('large_dataset.initial_rows')::BIGINT
) AS series(id);

DO $benchmark$
DECLARE
    rows_now BIGINT;
    max_rows BIGINT := current_setting('large_dataset.maximum_rows')::BIGINT;
    next_rows BIGINT;
    affected_rows BIGINT;
    started_at TIMESTAMPTZ;
    elapsed INTERVAL;
    elapsed_ms BIGINT;
BEGIN
    rows_now := current_setting('large_dataset.initial_rows')::BIGINT;

    LOOP
        started_at := clock_timestamp();

        UPDATE large_migration_dataset
        SET payload = repeat(md5(payload || ':' || id::text), 8),
            updated_at = statement_timestamp();

        GET DIAGNOSTICS affected_rows = ROW_COUNT;
        elapsed := clock_timestamp() - started_at;
        elapsed_ms := (extract(epoch FROM elapsed) * 1000)::BIGINT;

        RAISE NOTICE 'LARGE_DATASET_UPDATE rows=% duration_ms=%',
            affected_rows, elapsed_ms;

        IF elapsed >= INTERVAL '1 second' THEN
            INSERT INTO large_dataset_benchmark_runs (measured_at, rows_affected, duration_ms)
            VALUES (started_at, affected_rows, elapsed_ms);
            RAISE NOTICE
                'One-second threshold met; dataset is ready for batch tests (batch_column=id, batch_size=10000)';
            EXIT;
        END IF;

        IF rows_now >= max_rows THEN
            RAISE EXCEPTION
                'UPDATE took % ms at % rows, below the 1000 ms requirement; raise maximum_rows and rerun',
                elapsed_ms, rows_now;
        END IF;

        next_rows := LEAST(rows_now * 2, max_rows);
        INSERT INTO large_migration_dataset (id, batch_value, payload, updated_at)
        SELECT id,
               0,
               repeat(md5('large-dataset-seed-' || id::text), 8),
               transaction_timestamp()
        FROM generate_series(rows_now + 1, next_rows) AS series(id);

        rows_now := next_rows;
        RAISE NOTICE 'Expanded dataset to % rows; benchmarking again', rows_now;
    END LOOP;
END;
$benchmark$;

SELECT run_id, measured_at, rows_affected, duration_ms
FROM large_dataset_benchmark_runs
ORDER BY run_id DESC
LIMIT 1;
