-- +goose Up
-- Scheduling domain: named scenarios each holding an editable candidate schedule
-- (lines + their per-day shift slots, controllers, and leave). The pure engine
-- packages never see these tables; the store maps rows to domain types.
CREATE TABLE scenarios (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    pp_start TEXT NOT NULL DEFAULT '2026-07-12', -- pay-period start (a Sunday); leave in this 14-day window drives OT
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE lines (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    scenario_id INTEGER NOT NULL REFERENCES scenarios(id) ON DELETE CASCADE,
    code TEXT NOT NULL,                    -- display label, e.g. "E1"
    sort_order INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_lines_scenario_id ON lines(scenario_id);

CREATE TABLE line_days (                   -- a row = a worked slot; no row = RDO
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    line_id INTEGER NOT NULL REFERENCES lines(id) ON DELETE CASCADE,
    day_index INTEGER NOT NULL,            -- 0=Sun .. 6=Sat
    start_min INTEGER NOT NULL,            -- minutes since midnight
    duration_min INTEGER NOT NULL,
    UNIQUE(line_id, day_index)
);

CREATE TABLE controllers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    scenario_id INTEGER NOT NULL REFERENCES scenarios(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    qual_ap INTEGER NOT NULL DEFAULT 0,
    qual_lc INTEGER NOT NULL DEFAULT 0,
    qual_cic INTEGER NOT NULL DEFAULT 0,
    line_id INTEGER REFERENCES lines(id) ON DELETE SET NULL, -- nullable = unassigned
    sort_order INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_controllers_scenario_id ON controllers(scenario_id);

CREATE TABLE leave (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    scenario_id INTEGER NOT NULL REFERENCES scenarios(id) ON DELETE CASCADE,
    controller_id INTEGER NOT NULL REFERENCES controllers(id) ON DELETE CASCADE,
    leave_date TEXT NOT NULL,              -- 'YYYY-MM-DD'
    hours_min INTEGER NOT NULL,            -- leave hours, in minutes
    leave_type TEXT NOT NULL               -- 'annual' | 'sick' | 'bid'
);
CREATE INDEX idx_leave_scenario_id ON leave(scenario_id);

-- +goose Down
DROP TABLE leave;
DROP TABLE controllers;
DROP TABLE line_days;
DROP TABLE lines;
DROP TABLE scenarios;
