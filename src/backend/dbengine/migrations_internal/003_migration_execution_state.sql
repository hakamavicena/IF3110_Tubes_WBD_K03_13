ALTER TABLE migrations
    ADD COLUMN IF NOT EXISTS active_state TEXT NOT NULL DEFAULT 'not_applied',
    ADD COLUMN IF NOT EXISTS active_execution_id BIGINT;

UPDATE migrations
SET active_state = 'applied'
WHERE status = 'success'
  AND active_state = 'not_applied';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'migrations_active_state_check'
          AND conrelid = 'migrations'::regclass
    ) THEN
        ALTER TABLE migrations
            ADD CONSTRAINT migrations_active_state_check
            CHECK (active_state IN (
                'not_applied',
                'applied',
                'rolled_back',
                'partial',
                'partially_reverted'
            ));
    END IF;
END $$;