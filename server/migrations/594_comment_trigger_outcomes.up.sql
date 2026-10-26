ALTER TABLE comment
    ADD COLUMN trigger_outcomes JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN trigger_attempt_id UUID;

ALTER TABLE comment ALTER COLUMN trigger_attempt_id SET DEFAULT gen_random_uuid();
