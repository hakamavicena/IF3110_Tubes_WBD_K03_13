ALTER TABLE migration_history
    DROP CONSTRAINT IF EXISTS migration_history_status_check;

ALTER TABLE migration_history
    ADD CONSTRAINT migration_history_status_check
    CHECK (status IN ('success', 'failed'));