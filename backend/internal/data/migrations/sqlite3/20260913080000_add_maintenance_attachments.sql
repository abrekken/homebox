-- +goose Up
ALTER TABLE attachments ADD COLUMN maintenance_entry_attachments uuid REFERENCES maintenance_entries(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_attachment_maintenance_entry
    ON attachments (maintenance_entry_attachments);

-- +goose Down
DROP INDEX IF EXISTS idx_attachment_maintenance_entry;
ALTER TABLE attachments DROP COLUMN maintenance_entry_attachments;
