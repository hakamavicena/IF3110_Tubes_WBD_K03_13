ALTER TABLE migrations
    ADD COLUMN IF NOT EXISTS sql_down TEXT,
    ADD COLUMN IF NOT EXISTS is_irreversible BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS irreversible_reason TEXT,
    ADD COLUMN IF NOT EXISTS is_immutable BOOLEAN NOT NULL DEFAULT FALSE;

-- Migration M1 yang tidak memiliki SQL Down ditandai irreversible.
UPDATE migrations
SET is_irreversible = TRUE,
    irreversible_reason = COALESCE(
        NULLIF(BTRIM(irreversible_reason), ''),
        'Legacy migration has no safely recorded rollback definition.'
    )
WHERE (
    NOT is_irreversible
    AND NULLIF(BTRIM(sql_down), '') IS NULL
)
OR (
    is_irreversible
    AND NULLIF(BTRIM(irreversible_reason), '') IS NULL
);

UPDATE migrations
SET is_immutable = TRUE
WHERE status IN ('running', 'success', 'failed')
  AND NOT is_immutable;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'migrations_definition_reversible_check'
          AND conrelid = 'migrations'::regclass
    ) THEN
        ALTER TABLE migrations
            ADD CONSTRAINT migrations_definition_reversible_check
            CHECK (
                (
                    is_irreversible
                    AND NULLIF(BTRIM(irreversible_reason), '') IS NOT NULL
                )
                OR
                (
                    NOT is_irreversible
                    AND NULLIF(BTRIM(sql_down), '') IS NOT NULL
                )
            );
    END IF;
END $$;