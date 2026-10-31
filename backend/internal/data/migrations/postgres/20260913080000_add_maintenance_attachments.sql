-- +goose Up
ALTER TABLE attachments ADD COLUMN maintenance_entry_attachments uuid REFERENCES maintenance_entries(id) ON DELETE SET NULL;

CREATE INDEX idx_attachments_maintenance_entry_id ON attachments (maintenance_entry_attachments);

-- +goose Down
DROP INDEX IF EXISTS idx_attachments_maintenance_entry_id;
ALTER TABLE attachments DROP COLUMN maintenance_entry_attachments;
