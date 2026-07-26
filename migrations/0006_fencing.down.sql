ALTER TABLE deliveries DROP COLUMN cell_id;
ALTER TABLE couriers DROP COLUMN handoff_state;
ALTER TABLE couriers DROP COLUMN courier_session_epoch;
ALTER TABLE couriers DROP COLUMN home_cell;
DROP TABLE assignment_history;
DROP TABLE active_assignments;
DROP TABLE dispatch_fences;
