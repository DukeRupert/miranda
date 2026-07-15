-- Scenarios -----------------------------------------------------------------

-- name: ListScenarios :many
SELECT id, name, pp_start, created_at, updated_at
FROM scenarios
ORDER BY id;

-- name: GetScenario :one
SELECT id, name, pp_start, created_at, updated_at
FROM scenarios
WHERE id = ?;

-- name: CreateScenario :one
INSERT INTO scenarios (name, pp_start)
VALUES (?, ?)
RETURNING id, name, pp_start, created_at, updated_at;

-- name: UpdateScenario :exec
UPDATE scenarios
SET name = ?, pp_start = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: DeleteScenario :exec
DELETE FROM scenarios WHERE id = ?;

-- Lines ---------------------------------------------------------------------

-- name: ListLinesByScenario :many
SELECT id, scenario_id, code, sort_order
FROM lines
WHERE scenario_id = ?
ORDER BY sort_order, id;

-- name: GetLine :one
SELECT id, scenario_id, code, sort_order
FROM lines
WHERE id = ?;

-- name: CreateLine :one
INSERT INTO lines (scenario_id, code, sort_order)
VALUES (?, ?, ?)
RETURNING id, scenario_id, code, sort_order;

-- name: UpdateLine :exec
UPDATE lines
SET code = ?, sort_order = ?
WHERE id = ?;

-- name: DeleteLine :exec
DELETE FROM lines WHERE id = ?;

-- Line days (worked slots) --------------------------------------------------

-- name: ListLineDaysByScenario :many
SELECT ld.id, ld.line_id, ld.day_index, ld.start_min, ld.duration_min
FROM line_days ld
JOIN lines l ON ld.line_id = l.id
WHERE l.scenario_id = ?
ORDER BY ld.line_id, ld.day_index;

-- name: DeleteLineDays :exec
DELETE FROM line_days WHERE line_id = ?;

-- name: CreateLineDay :exec
INSERT INTO line_days (line_id, day_index, start_min, duration_min)
VALUES (?, ?, ?, ?);

-- Controllers ---------------------------------------------------------------

-- name: ListControllersByScenario :many
SELECT id, scenario_id, name, qual_ap, qual_lc, qual_cic, line_id, sort_order
FROM controllers
WHERE scenario_id = ?
ORDER BY sort_order, id;

-- name: CreateController :one
INSERT INTO controllers (scenario_id, name, qual_ap, qual_lc, qual_cic, line_id, sort_order)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING id, scenario_id, name, qual_ap, qual_lc, qual_cic, line_id, sort_order;

-- name: UpdateController :exec
UPDATE controllers
SET name = ?, qual_ap = ?, qual_lc = ?, qual_cic = ?, line_id = ?
WHERE id = ?;

-- name: DeleteController :exec
DELETE FROM controllers WHERE id = ?;

-- Leave ---------------------------------------------------------------------

-- name: ListLeaveByScenario :many
SELECT id, scenario_id, controller_id, leave_date, hours_min, leave_type
FROM leave
WHERE scenario_id = ?
ORDER BY leave_date, id;

-- name: CreateLeave :exec
INSERT INTO leave (scenario_id, controller_id, leave_date, hours_min, leave_type)
VALUES (?, ?, ?, ?, ?);

-- name: DeleteLeave :exec
DELETE FROM leave WHERE id = ?;
