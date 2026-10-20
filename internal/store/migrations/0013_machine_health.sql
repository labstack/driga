-- +goose Up
ALTER TABLE procs ADD COLUMN footprint INTEGER NOT NULL DEFAULT 0;
ALTER TABLE procs ADD COLUMN swap INTEGER NOT NULL DEFAULT 0;
ALTER TABLE procs ADD COLUMN memory_measure TEXT NOT NULL DEFAULT 'rss';
ALTER TABLE machine_samples ADD COLUMN pressure_level TEXT NOT NULL DEFAULT 'unknown';

-- +goose Down
ALTER TABLE machine_samples DROP COLUMN pressure_level;
ALTER TABLE procs DROP COLUMN memory_measure;
ALTER TABLE procs DROP COLUMN swap;
ALTER TABLE procs DROP COLUMN footprint;
