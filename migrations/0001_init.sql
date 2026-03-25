CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TYPE reminder_status AS ENUM ('pending', 'completed', 'cancelled');

CREATE TABLE reminders (
    id            UUID            PRIMARY KEY DEFAULT uuid_generate_v4(),
    thread_id     UUID            NOT NULL,
    identity_id   UUID            NOT NULL,
    note          TEXT            NOT NULL,
    status        reminder_status NOT NULL DEFAULT 'pending',
    at            TIMESTAMPTZ     NOT NULL,
    created_at    TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    completed_at  TIMESTAMPTZ,
    cancelled_at  TIMESTAMPTZ
);

CREATE INDEX reminders_thread_id_status_idx ON reminders (thread_id, status);
CREATE INDEX reminders_status_at_idx ON reminders (status, at) WHERE status = 'pending';
