# Task: Allow attachments on maintenance entries (backend only)

You are working in Homebox, a Go (1.2x) home-inventory app. Backend uses chi routing,
Ent ORM (code-generated), goose SQL migrations for SQLite and Postgres, and swaggo docs.
Items are stored as `Entity`. An entity can have many `MaintenanceEntry` records.
Today attachments (files) can only be linked to an entity. Add the ability to attach
files directly to a maintenance entry.

This prompt covers the BACKEND ONLY. Do not modify anything under `frontend/`.

## Acceptance criteria

1. A user can upload a file to a maintenance entry:
   `POST /v1/maintenance/{id}/attachments` (multipart: `file` required, `name` required,
   `type` optional). Returns 201 with the updated maintenance entry.
2. A user can delete one: `DELETE /v1/maintenance/{id}/attachments/{attachment_id}` → 204.
3. Maintenance entry responses (`repo.MaintenanceEntry`, and therefore
   `MaintenanceEntryWithDetails`) include `attachments: ItemAttachment[]`
   (empty array, never null).
4. Downloading a maintenance attachment works through the EXISTING route
   `GET /v1/entities/{itemId}/attachments/{attachment_id}` with no changes to it.
5. Maintenance attachments do NOT appear in the item's own `attachments` list
   (`EntityOut.Attachments`).
6. Deleting a maintenance entry deletes its attachments, including the stored files
   (not just the DB rows).
7. Group isolation: a user in group A cannot upload to, list, or delete attachments on a
   maintenance entry in group B. Return not-found in that case, same as the existing code.
8. `primary` is always false for maintenance attachments; thumbnails still work as they
   do for item photos.

## Design (follow this, do not invent a different one)

- Add an OPTIONAL edge from `Attachment` to `MaintenanceEntry`:
  - `maintenance_entry.go` Edges: `edge.To("attachments", Attachment.Type)`
  - `attachment.go` Edges: `edge.From("maintenance_entry", MaintenanceEntry.Type).Ref("attachments").Unique()`
- A maintenance attachment ALSO keeps its `entity` edge set to the parent item.
  This is intentional: every existing group-ownership check in `AttachmentRepo`
  (`Get`, `Update`, `Delete`) goes through `attachment.HasEntityWith(entity.HasGroupWith(...))`,
  so those checks keep working for free and the download route (criterion 4) needs no change.
- Because of that, criterion 5 requires filtering: wherever the item's attachments are loaded
  for `EntityOut` in `repo_entities.go` (e.g. `WithAttachments()`), exclude attachments where
  `attachment.HasMaintenanceEntry()` is true. Leave the photo/thumbnail logic otherwise alone.

## Files to change

Backend root is `backend/`.

1. `internal/data/ent/schema/attachment.go` and `maintenance_entry.go`: add the edges above.
2. Run `task db:generate` (or `cd backend/internal && go generate ./...`). NEVER hand-edit
   anything under `internal/data/ent/` except the `schema/` folder.
3. Migrations: add ONE new goose file in EACH of
   `internal/data/migrations/sqlite3/` and `internal/data/migrations/postgres/`,
   named with a timestamp later than the latest existing one (`20260512130000_add_exports.sql`,
   `20260512130001_add_exports.sql`), e.g. `20260601000000_add_maintenance_attachments.sql`.
   Add a nullable `maintenance_entry_attachments` uuid column on `attachments` with a FK to
   `maintenance_entries(id)` `ON DELETE SET NULL`, plus an index. Match the style of the
   existing files (`-- +goose Up` header). Check the column name that Ent generates and use that.
   SQLite cannot `ALTER TABLE ... ADD CONSTRAINT`; look at how earlier sqlite3 migrations add FK
   columns and copy that approach.
4. `internal/data/repo/repo_item_attachments.go`: add
   `CreateForMaintenance(ctx, gid, maintenanceID uuid.UUID, doc ItemCreateAttachment, typ attachment.Type) (*ent.Attachment, error)`.
   It must look up the maintenance entry scoped to the group
   (`maintenanceentry.ID(id), maintenanceentry.HasEntityWith(entity.HasGroupWith(group.ID(gid)))`),
   then reuse the same upload/thumbnail path as the existing `Create`, setting both
   `SetEntityID(entry.EntityID)` and `SetMaintenanceEntryID(maintenanceID)`.
   Refactor shared code out of `Create` instead of copy-pasting it.
5. `internal/data/repo/repo_maintenance_entry.go`:
   - Add `Attachments []ItemAttachment` to `MaintenanceEntry` and map it in `mapMaintenanceEntry`
     using the existing `ToItemAttachment` helper.
   - Add `.WithAttachments()` to every query that returns entries (including in `repo_maintenance.go`).
   - `Delete`: before deleting the entry, delete each of its attachments via
     `AttachmentRepo.Delete` so files are removed from blob storage. The repo needs access to
     the `AttachmentRepo`; wire it in `repos_all.go`.
6. `app/api/handlers/v1/`: add handlers in a new file `v1_ctrl_maint_entry_attachments.go`.
   Model the create handler on `HandleEntityAttachmentCreate` in
   `v1_ctrl_entities_attachments.go` (multipart parsing, `sanitizeAttachmentName`, type
   detection from extension, field errors → 422). Include swaggo godoc comments like the
   existing handlers, tag `Maintenance`.
7. `app/api/routes.go`: register the two routes next to the existing `/maintenance/{id}` routes,
   using the same `userMW` chain.
8. Run `task swag` then `task typescript-types` to regenerate docs and
   `frontend/lib/api/types/data-contracts.ts` (generated file changes are allowed).

## Constraints

- Keep the existing OpenTelemetry span pattern (`otel.Tracer("data").Start(...)` in repo,
  `startEntityCtrlSpan` / `recordCtrlSpanError` in handlers) in new code.
- Do not change existing public function signatures unless necessary; if you must, update all callers.
- Out of scope: exports/import, external-link attachments, updating attachment metadata,
  frontend UI.
- No new dependencies.

## Tests

Add tests alongside the existing ones, following their setup helpers:
- `internal/data/repo/repo_maintenance_entry_test.go`: create entry → add attachment →
  entry returns it; item's `EntityOut.Attachments` does NOT include it; delete entry → attachment gone.
- `internal/data/repo/repo_authz_test.go`: cross-group create/delete on a maintenance attachment fails.

## Definition of done

From `backend/`: `go build ./...`, `go test ./...`, and `task go:lint` all pass.

## Output format

For each file, give the full path and either the complete new file or a unified diff.
List any commands I need to run, in order. If something in this spec conflicts with what
you see in the code, say so instead of guessing.

## Reference code (current)
backend/internal/data/ent/schema/attachment.go:

package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/sysadminsmedia/homebox/backend/internal/data/ent/schema/mixins"
)

// Attachment holds the schema definition for the Attachment entity.
type Attachment struct {
	ent.Schema
}

func (Attachment) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.BaseMixin{},
	}
}

// Fields of the Attachment.
func (Attachment) Fields() []ent.Field {
	return []ent.Field{
		field.Enum("type").Values("photo", "manual", "warranty", "attachment", "receipt", "thumbnail").Default("attachment"),
		field.Bool("primary").Default(false),
		field.String("title").Default(""),
		field.String("path").Default(""),
		field.String("mime_type").Default("application/octet-stream"),
	}
}

// Edges of the Attachment.
func (Attachment) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("entity", Entity.Type).
			Ref("attachments").
			Unique(),
		edge.To("thumbnail", Attachment.Type).
			Unique(),
	}
}

backend/internal/data/ent/schema/maintenance_entry.go:

package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
	"github.com/sysadminsmedia/homebox/backend/internal/data/ent/schema/mixins"
)

type MaintenanceEntry struct {
	ent.Schema
}

func (MaintenanceEntry) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.BaseMixin{},
	}
}

func (MaintenanceEntry) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("entity_id", uuid.UUID{}),
		field.Time("date").
			Optional(),
		field.Time("scheduled_date").
			Optional(),
		field.String("name").
			MaxLen(255).
			NotEmpty(),
		field.String("description").
			MaxLen(2500).
			Optional(),
		field.Float("cost").
			Default(0.0),
	}
}

// Edges of the MaintenanceEntry.
func (MaintenanceEntry) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("entity", Entity.Type).
			Field("entity_id").
			Ref("maintenance_entries").
			Required().
			Unique(),
	}
}

backend/internal/data/repo/repo_maintenance_entry.go:

package repo

import (
	"context"
	"errors"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	"github.com/sysadminsmedia/homebox/backend/internal/data/ent"
	"github.com/sysadminsmedia/homebox/backend/internal/data/ent/entity"
	"github.com/sysadminsmedia/homebox/backend/internal/data/ent/group"
	"github.com/sysadminsmedia/homebox/backend/internal/data/ent/maintenanceentry"
	"github.com/sysadminsmedia/homebox/backend/internal/data/types"
)

// MaintenanceEntryRepository is a repository for maintenance entries that are
// associated with an item in the database. An entry represents a maintenance event
// that has been performed on an item.
type MaintenanceEntryRepository struct {
	db *ent.Client
}

type MaintenanceEntryCreate struct {
	CompletedDate types.Date `json:"completedDate"`
	ScheduledDate types.Date `json:"scheduledDate"`
	Name          string     `json:"name"          validate:"required"`
	Description   string     `json:"description"`
	Cost          float64    `json:"cost,string"`
}

func (mc MaintenanceEntryCreate) Validate() error {
	if mc.CompletedDate.Time().IsZero() && mc.ScheduledDate.Time().IsZero() {
		return errors.New("either completedDate or scheduledDate must be set")
	}
	return nil
}

type MaintenanceEntryUpdate struct {
	CompletedDate types.Date `json:"completedDate"`
	ScheduledDate types.Date `json:"scheduledDate"`
	Name          string     `json:"name"`
	Description   string     `json:"description"`
	Cost          float64    `json:"cost,string"`
}

func (mu MaintenanceEntryUpdate) Validate() error {
	if mu.CompletedDate.Time().IsZero() && mu.ScheduledDate.Time().IsZero() {
		return errors.New("either completedDate or scheduledDate must be set")
	}
	return nil
}

type (
	MaintenanceEntry struct {
		ID            uuid.UUID  `json:"id"`
		CompletedDate types.Date `json:"completedDate"`
		ScheduledDate types.Date `json:"scheduledDate"`
		Name          string     `json:"name"`
		Description   string     `json:"description"`
		Cost          float64    `json:"cost,string"`
	}
)

var (
	mapMaintenanceEntryErr  = mapTErrFunc(mapMaintenanceEntry)
	mapEachMaintenanceEntry = mapTEachFunc(mapMaintenanceEntry)
)

func mapMaintenanceEntry(entry *ent.MaintenanceEntry) MaintenanceEntry {
	return MaintenanceEntry{
		ID:            entry.ID,
		CompletedDate: types.DateFromDBTime(entry.Date),
		ScheduledDate: types.DateFromDBTime(entry.ScheduledDate),
		Name:          entry.Name,
		Description:   entry.Description,
		Cost:          entry.Cost,
	}
}

func (r *MaintenanceEntryRepository) GetScheduled(ctx context.Context, gid uuid.UUID, dt types.Date) ([]MaintenanceEntry, error) {
	entries, err := r.db.MaintenanceEntry.Query().
		Where(
			maintenanceentry.HasEntityWith(
				entity.HasGroupWith(group.ID(gid)),
			),
			maintenanceentry.ScheduledDate(dt.Time()),
			maintenanceentry.Or(
				maintenanceentry.DateIsNil(),
				maintenanceentry.DateEQ(time.Time{}),
			),
		).
		All(ctx)

	if err != nil {
		return nil, err
	}

	return mapEachMaintenanceEntry(entries), nil
}

func (r *MaintenanceEntryRepository) Create(ctx context.Context, gid, itemID uuid.UUID, input MaintenanceEntryCreate) (MaintenanceEntry, error) {
	// Verify the target item belongs to the caller's group before creating an
	// entry against it. Without this check, a caller in group A could POST a
	// maintenance entry to any item UUID guessed in group B; the entry would
	// then surface in B's maintenance log with no audit trail.
	owned, err := r.db.Entity.Query().
		Where(entity.ID(itemID), entity.HasGroupWith(group.ID(gid))).
		Exist(ctx)
	if err != nil {
		return MaintenanceEntry{}, err
	}
	if !owned {
		return MaintenanceEntry{}, &ent.NotFoundError{}
	}

	item, err := r.db.MaintenanceEntry.Create().
		SetEntityID(itemID).
		SetDate(input.CompletedDate.Time()).
		SetScheduledDate(input.ScheduledDate.Time()).
		SetName(input.Name).
		SetDescription(input.Description).
		SetCost(input.Cost).
		Save(ctx)

	return mapMaintenanceEntryErr(item, err)
}

func (r *MaintenanceEntryRepository) Update(ctx context.Context, gid uuid.UUID, id uuid.UUID, input MaintenanceEntryUpdate) (MaintenanceEntry, error) {
	owned, err := r.db.MaintenanceEntry.Query().Where(
		maintenanceentry.ID(id),
		maintenanceentry.HasEntityWith(entity.HasGroupWith(group.ID(gid))),
	).Exist(ctx)
	if err != nil {
		return MaintenanceEntry{}, err
	}
	if !owned {
		return MaintenanceEntry{}, &ent.NotFoundError{}
	}

	item, err := r.db.MaintenanceEntry.UpdateOneID(id).
		SetDate(input.CompletedDate.Time()).
		SetScheduledDate(input.ScheduledDate.Time()).
		SetName(input.Name).
		SetDescription(input.Description).
		SetCost(input.Cost).
		Save(ctx)

	return mapMaintenanceEntryErr(item, err)
}

func (r *MaintenanceEntryRepository) GetMaintenanceByItemID(ctx context.Context, groupID, itemID uuid.UUID, filters MaintenanceFilters) ([]MaintenanceEntryWithDetails, error) {
	query := r.db.MaintenanceEntry.Query().Where(
		maintenanceentry.EntityID(itemID),
		maintenanceentry.HasEntityWith(
			entity.HasGroupWith(group.IDEQ(groupID)),
		),
	)
	switch filters.Status {
	case MaintenanceFilterStatusScheduled:
		query = query.Where(maintenanceentry.Or(
			maintenanceentry.DateIsNil(),
			maintenanceentry.DateEQ(time.Time{}),
			maintenanceentry.DateGT(time.Now()),
		))
		// Sort scheduled entries by ascending scheduled date
		query = query.Order(
			maintenanceentry.ByScheduledDate(sql.OrderAsc()),
		)
	case MaintenanceFilterStatusCompleted:
		query = query.Where(
			maintenanceentry.Not(maintenanceentry.Or(
				maintenanceentry.DateIsNil(),
				maintenanceentry.DateEQ(time.Time{}),
				maintenanceentry.DateGT(time.Now()),
			)))
		// Sort completed entries by descending completion date
		query = query.Order(
			maintenanceentry.ByDate(sql.OrderDesc()),
		)
	default:
		// Sort entries by default by scheduled and maintenance date in descending order
		query = query.Order(
			maintenanceentry.ByScheduledDate(sql.OrderDesc()),
			maintenanceentry.ByDate(sql.OrderDesc()),
		)
	}
	entries, err := query.WithEntity().All(ctx)

	if err != nil {
		return []MaintenanceEntryWithDetails{}, err
	}

	return mapEachMaintenanceEntryWithDetails(entries), nil
}

func (r *MaintenanceEntryRepository) Delete(ctx context.Context, gid uuid.UUID, id uuid.UUID) error {
	deleted, err := r.db.MaintenanceEntry.Delete().Where(
		maintenanceentry.ID(id),
		maintenanceentry.HasEntityWith(entity.HasGroupWith(group.ID(gid))),
	).Exec(ctx)
	if err != nil {
		return err
	}
	if deleted == 0 {
		return &ent.NotFoundError{}
	}
	return nil
}

backend/internal/data/repo/repo_maintenance.go:

package repo

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/sysadminsmedia/homebox/backend/internal/data/ent"
	"github.com/sysadminsmedia/homebox/backend/internal/data/ent/entity"
	"github.com/sysadminsmedia/homebox/backend/internal/data/ent/group"
	"github.com/sysadminsmedia/homebox/backend/internal/data/ent/maintenanceentry"
)

type (
	MaintenanceEntryWithDetails struct {
		MaintenanceEntry
		ItemName string    `json:"itemName"`
		ItemID   uuid.UUID `json:"itemID"`
	}
)

var (
	mapEachMaintenanceEntryWithDetails = mapTEachFunc(mapMaintenanceEntryWithDetails)
)

func mapMaintenanceEntryWithDetails(entry *ent.MaintenanceEntry) MaintenanceEntryWithDetails {
	return MaintenanceEntryWithDetails{
		MaintenanceEntry: mapMaintenanceEntry(entry),
		ItemName:         entry.Edges.Entity.Name,
		ItemID:           entry.EntityID,
	}
}

type MaintenanceFilterStatus string

const (
	MaintenanceFilterStatusScheduled MaintenanceFilterStatus = "scheduled"
	MaintenanceFilterStatusCompleted MaintenanceFilterStatus = "completed"
	MaintenanceFilterStatusBoth      MaintenanceFilterStatus = "both"
)

type MaintenanceFilters struct {
	Status MaintenanceFilterStatus `json:"status" schema:"status"`
}

func (r *MaintenanceEntryRepository) GetAllMaintenance(ctx context.Context, groupID uuid.UUID, filters MaintenanceFilters) ([]MaintenanceEntryWithDetails, error) {
	query := r.db.MaintenanceEntry.Query().Where(
		maintenanceentry.HasEntityWith(
			entity.HasGroupWith(group.IDEQ(groupID)),
		),
	)

	switch filters.Status {
	case MaintenanceFilterStatusScheduled:
		query = query.Where(maintenanceentry.Or(
			maintenanceentry.DateIsNil(),
			maintenanceentry.DateEQ(time.Time{}),
		))
	case MaintenanceFilterStatusCompleted:
		query = query.Where(
			maintenanceentry.Not(maintenanceentry.Or(
				maintenanceentry.DateIsNil(),
				maintenanceentry.DateEQ(time.Time{})),
			))
	case MaintenanceFilterStatusBoth:
		// No additional filters needed
	default:
		return nil, fmt.Errorf("unknown status %s", filters.Status)
	}
	entries, err := query.WithEntity().Order(maintenanceentry.ByScheduledDate()).All(ctx)

	if err != nil {
		return nil, err
	}

	return mapEachMaintenanceEntryWithDetails(entries), nil
}

backend/internal/data/repo/repo_item_attachments.go (lines 1-455):

package repo

import (
	"bytes"
	"context"
	"crypto/md5"
	"fmt"
	"image"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/evanoberholster/imagemeta"
	"github.com/gen2brain/avif"
	"github.com/gen2brain/heic"
	"github.com/gen2brain/jpegxl"
	"github.com/gen2brain/webp"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/sysadminsmedia/homebox/backend/internal/data/ent"
	"github.com/sysadminsmedia/homebox/backend/internal/data/ent/attachment"
	"github.com/sysadminsmedia/homebox/backend/internal/data/ent/entity"
	"github.com/sysadminsmedia/homebox/backend/internal/data/ent/group"
	"github.com/sysadminsmedia/homebox/backend/internal/sys/config"
	"github.com/sysadminsmedia/homebox/backend/pkgs/utils"
	"github.com/zeebo/blake3"
	"go.opentelemetry.io/otel"
	"golang.org/x/image/draw"

	"gocloud.dev/blob"
	_ "gocloud.dev/blob/azureblob"
	_ "gocloud.dev/blob/fileblob"
	_ "gocloud.dev/blob/gcsblob"
	_ "gocloud.dev/blob/memblob"
	_ "gocloud.dev/blob/s3blob"

	"gocloud.dev/pubsub"
	_ "gocloud.dev/pubsub/awssnssqs"
	_ "gocloud.dev/pubsub/azuresb"
	_ "gocloud.dev/pubsub/gcppubsub"
	_ "gocloud.dev/pubsub/kafkapubsub"
	_ "gocloud.dev/pubsub/mempubsub"
	_ "gocloud.dev/pubsub/natspubsub"
	_ "gocloud.dev/pubsub/rabbitpubsub"
)

// AttachmentRepo is a repository for Attachments table that links Items to their
// associated files while also specifying the type of the attachment.
type AttachmentRepo struct {
	db         *ent.Client
	storage    config.Storage
	pubSubConn string
	thumbnail  config.Thumbnail
}

type (
	ItemAttachment struct {
		ID        uuid.UUID       `json:"id"`
		CreatedAt time.Time       `json:"createdAt"`
		UpdatedAt time.Time       `json:"updatedAt"`
		Type      string          `json:"type"`
		Primary   bool            `json:"primary"`
		Path      string          `json:"path"`
		Title     string          `json:"title"`
		MimeType  string          `json:"mimeType,omitempty"`
		Thumbnail *ent.Attachment `json:"thumbnail,omitempty"`
	}

	ItemAttachmentUpdate struct {
		ID      uuid.UUID `json:"-"`
		Type    string    `json:"type"`
		Title   string    `json:"title"`
		Primary bool      `json:"primary"`
	}

	ItemCreateAttachment struct {
		Title   string    `json:"title"`
		Content io.Reader `json:"content"`
	}
)

const MimeTypeLinkURL = "link/url"

var externalLinkMimeTypes = []string{
	MimeTypeLinkURL,
}

func MimeTypeForSourceType(sourceType string) (string, bool) {
	switch sourceType {
	case "link":
		return MimeTypeLinkURL, true
	default:
		return "", false
	}
}

func isExternalLink(mimeType string) bool {
	for _, m := range externalLinkMimeTypes {
		if m == mimeType {
			return true
		}
	}
	return false
}

func ToItemAttachment(attachment *ent.Attachment) ItemAttachment {
	return ItemAttachment{
		ID:        attachment.ID,
		CreatedAt: attachment.CreatedAt,
		UpdatedAt: attachment.UpdatedAt,
		Type:      attachment.Type.String(),
		Primary:   attachment.Primary,
		Path:      attachment.Path,
		Title:     attachment.Title,
		MimeType:  attachment.MimeType,
		Thumbnail: attachment.QueryThumbnail().FirstX(context.Background()),
	}
}

// normalizePath converts backslashes to forward slashes and trims slashes from both ends
// This ensures consistent path separators for blob storage which expects forward slashes
func normalizePath(path string) string {
	path = strings.ReplaceAll(path, "\\", "/")
	return strings.Trim(path, "/")
}

// legacyFlatPathToken is the gocloud fileblob escape sequence for a backslash
// (0x5c). On Windows, pre-v0.22.1 attachment keys built with filepath.Join used
// "\" as the path separator; fileblob escaped that to this token, so files were
// stored as flat names at the bucket root. v0.22.1+ uses "/" so files now live
// in real subdirectories.
const legacyFlatPathToken = "__0x5c__"

// bucketLocalDir returns the absolute on-disk directory backing the fileblob
// bucket, or "" if the storage backend is not a file:// URL.
func (r *AttachmentRepo) bucketLocalDir() (string, error) {
	cs := r.storage.ConnString
	if !strings.HasPrefix(cs, "file://") {
		return "", nil
	}
	// Homebox-specific shortcut for "relative to cwd".
	if strings.HasPrefix(cs, "file:///./") {
		return filepath.Abs(strings.TrimPrefix(cs, "file:///./"))
	}
	raw := strings.TrimPrefix(cs, "file://")
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		raw = raw[:i]
	}
	// On Windows a file URL looks like file:///C:/foo, so strip the empty-host
	// leading slash before treating the rest as a native path.
	if runtime.GOOS == "windows" && len(raw) >= 3 && raw[0] == '/' && raw[2] == ':' {
		raw = raw[1:]
	}
	return filepath.Abs(raw)
}

// MigrateLegacyFlatPaths renames attachment files written by older homebox
// versions on Windows from a flat-escaped layout into a real subdirectory
// layout. It is a no-op when the storage backend is not file:// or when no
// matching files are found, so it is safe to run on every startup. Callers
// should gate invocation on runtime.GOOS == "windows" since no other platform
// can produce these files.
func (r *AttachmentRepo) MigrateLegacyFlatPaths() error {
	root, err := r.bucketLocalDir()
	if err != nil {
		return err
	}
	if root == "" {
		return nil
	}
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return nil
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("read bucket root %s: %w", root, err)
	}

	moved, skipped := 0, 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.Contains(name, legacyFlatPathToken) {
			continue
		}

		decoded := strings.ReplaceAll(name, legacyFlatPathToken, string(filepath.Separator))
		oldPath := filepath.Join(root, name)
		newPath := filepath.Join(root, decoded)

		if _, statErr := os.Stat(newPath); statErr == nil {
			log.Warn().Str("source", oldPath).Str("target", newPath).
				Msg("legacy attachment migration: target already exists, leaving source in place")
			skipped++
			continue
		} else if !os.IsNotExist(statErr) {
			log.Err(statErr).Str("target", newPath).Msg("legacy attachment migration: failed to stat target")
			skipped++
			continue
		}

		if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
			log.Err(err).Str("dir", filepath.Dir(newPath)).Msg("legacy attachment migration: failed to create parent directory")
			skipped++
			continue
		}
		if err := os.Rename(oldPath, newPath); err != nil {
			log.Err(err).Str("source", oldPath).Str("target", newPath).Msg("legacy attachment migration: failed to rename file")
			skipped++
			continue
		}
		moved++
	}

	if moved > 0 || skipped > 0 {
		log.Info().Int("moved", moved).Int("skipped", skipped).
			Msg("migrated legacy flat-encoded attachment files")
	}
	return nil
}

func (r *AttachmentRepo) path(gid uuid.UUID, hash string) string {
	// Always use forward slashes for consistency across platforms
	// This ensures paths are stored in the database with forward slashes
	return fmt.Sprintf("%s/documents/%s", gid.String(), hash)
}

func (r *AttachmentRepo) fullPath(relativePath string) string {
	// Normalize path separators to forward slashes for blob storage
	// The blob library expects forward slashes in keys regardless of OS
	normalizedRelativePath := normalizePath(relativePath)

	// Always use forward slashes when joining paths for blob storage
	if r.storage.PrefixPath == "" {
		return normalizedRelativePath
	}
	normalizedPrefix := normalizePath(r.storage.PrefixPath)

	if normalizedPrefix == "" {
		return normalizedRelativePath
	}

	return fmt.Sprintf("%s/%s", normalizedPrefix, normalizedRelativePath)
}

func (r *AttachmentRepo) GetFullPath(relativePath string) string {
	return r.fullPath(relativePath)
}

func (r *AttachmentRepo) GetConnString() string {
	// Handle the default case for file storage
	// which is file:///./ meaning relative to the current working directory
	if strings.HasPrefix(r.storage.ConnString, "file:///./") {
		dir, err := filepath.Abs(strings.TrimPrefix(r.storage.ConnString, "file:///./"))
		if runtime.GOOS == "windows" {
			dir = fmt.Sprintf("/%s", dir)
		}
		if err != nil {
			log.Err(err).Msg("failed to get absolute path for attachment directory")
			return r.storage.ConnString
		}
		return strings.ReplaceAll(fmt.Sprintf("file://%s?no_tmp_dir=true", dir), "\\", "/")
	} else if strings.HasPrefix(r.storage.ConnString, "file://") {
		// Handle the case for file storage with an absolute path
		// Convert Windows paths to a format compatible with fileblob
		// e.g. file:///C:/path/to/file becomes file:///C/path
		dir := strings.TrimPrefix(strings.ReplaceAll(r.storage.ConnString, "\\", "/"), "file://")
		if runtime.GOOS == "windows" {
			// Remove the colon from the drive letter (in case the user adds it)
			dir = strings.ReplaceAll(dir, ":", "")
			// Ensure the path starts with a slash for Windows compatibility
			dir = fmt.Sprintf("/%s", dir)
		}
		return fmt.Sprintf("file://%s", dir)
	}
	return r.storage.ConnString
}

func (r *AttachmentRepo) Create(ctx context.Context, itemID uuid.UUID, doc ItemCreateAttachment, typ attachment.Type, primary bool) (*ent.Attachment, error) {
	ctx, span := otel.Tracer("data").Start(ctx, "repo.AttachmentRepo.Create")
	defer span.End()

	tx, err := r.db.Tx(ctx)
	if err != nil {
		return nil, err
	}

	// If there is an error during file creation rollback the database
	defer func() {
		if v := recover(); v != nil {
			err := tx.Rollback()
			if err != nil {
				return
			}
		}
	}()

	bldrId := uuid.New()

	bldr := tx.Attachment.Create().
		SetID(bldrId).
		SetCreatedAt(time.Now()).
		SetUpdatedAt(time.Now()).
		SetType(typ).
		SetEntityID(itemID).
		SetTitle(doc.Title)

	if typ == attachment.TypePhoto && primary {
		bldr = bldr.SetPrimary(true)
		err := tx.Attachment.Update().
			Where(
				attachment.HasEntityWith(entity.ID(itemID)),
				attachment.IDNEQ(bldrId),
			).
			SetPrimary(false).
			Exec(ctx)
		if err != nil {
			log.Err(err).Msg("failed to remove primary from other attachments")
			err := tx.Rollback()
			if err != nil {
				return nil, err
			}
			return nil, err
		}
	} else if typ == attachment.TypePhoto {
		// Autoset primary to true if this is the first attachment
		// that is of type photo
		cnt, err := tx.Attachment.Query().
			Where(
				attachment.HasEntityWith(entity.ID(itemID)),
				attachment.TypeEQ(typ),
			).
			Count(ctx)
		if err != nil {
			log.Err(err).Msg("failed to count attachments")
			err := tx.Rollback()
			if err != nil {
				return nil, err
			}
			return nil, err
		}

		if cnt == 0 {
			bldr = bldr.SetPrimary(true)
		}
	}

	// Get the group ID for the item the attachment is being created for
	itemGroup, err := tx.Entity.Query().QueryGroup().Where(group.HasEntitiesWith(entity.ID(itemID))).First(ctx)
	if err != nil {
		log.Err(err).Msg("failed to get item group")
		err := tx.Rollback()
		if err != nil {
			return nil, err
		}
		return nil, err
	}

	// Upload the file to the storage bucket
	uploadResult, err := r.UploadFile(ctx, itemGroup, doc)
	if err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			return nil, rollbackErr
		}
		return nil, err
	}

	bldr = bldr.SetMimeType(uploadResult.ContentType)
	bldr = bldr.SetPath(uploadResult.Path)

	attachmentDb, err := bldr.Save(ctx)
	if err != nil {
		log.Err(err).Msg("failed to save attachment to database")
		err = tx.Rollback()
		if err != nil {
			return nil, err
		}
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		log.Err(err).Msg("failed to commit transaction")
		return nil, err
	}

	if r.thumbnail.Enabled {
		pubsubString, err := utils.GenerateSubPubConn(r.pubSubConn, "thumbnails")
		if err != nil {
			log.Err(err).Msg("failed to generate pubsub connection string")
			return nil, err
		}
		topic, err := pubsub.OpenTopic(ctx, pubsubString)
		if err != nil {
			log.Err(err).Msg("failed to open pubsub topic")
			return nil, err
		}

		err = topic.Send(ctx, &pubsub.Message{
			Body: []byte(fmt.Sprintf("attachment_created:%s", attachmentDb.ID.String())),
			Metadata: map[string]string{
				"group_id":      itemGroup.ID.String(),
				"attachment_id": attachmentDb.ID.String(),
				"title":         doc.Title,
				"path":          attachmentDb.Path,
			},
		})
		if err != nil {
			log.Err(err).Msg("failed to send message to topic")
			return nil, err
		}
	}

	return attachmentDb, nil
}

func (r *AttachmentRepo) CreateExternalLink(ctx context.Context, entityID uuid.UUID, externalID string, title string, mimeType string, attType attachment.Type) (*ent.Attachment, error) {
	ctx, span := otel.Tracer("data").Start(ctx, "repo.AttachmentRepo.CreateExternalLink")
	defer span.End()

	if attType == "" {
		attType = attachment.TypeAttachment
	}

	att, err := r.db.Attachment.Create().
		SetID(uuid.New()).
		SetCreatedAt(time.Now()).
		SetUpdatedAt(time.Now()).
		SetType(attType).
		SetEntityID(entityID).
		SetTitle(title).
		SetPath(externalID).
		SetMimeType(mimeType).
		SetPrimary(false).
		Save(ctx)
	if err != nil {
		return nil, err
	}

	return att, nil
}

backend/app/api/handlers/v1/v1_ctrl_entities_attachments.go (HandleEntityAttachmentCreate only):

// HandleEntityAttachmentCreate godoc
//
//	@Summary	Create Entity Attachment
//	@Tags		Entities Attachments
//	@Accept		multipart/form-data
//	@Produce	json
//	@Param		id		path		string	true	"Entity ID"
//	@Param		file	formData	file	true	"File attachment"
//	@Param		type	formData	string	false	"Type of file"
//	@Param		primary	formData	bool	false	"Is this the primary attachment"
//	@Param		name	formData	string	true	"name of the file including extension"
//	@Success	201		{object}	repo.EntityOut
//	@Failure	422		{object}	validate.ErrorResponse
//	@Router		/v1/entities/{id}/attachments [POST]
//	@Security	Bearer
func (ctrl *V1Controller) HandleEntityAttachmentCreate() errchain.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		spanCtx, span := startEntityCtrlSpan(r.Context(), "controller.V1.HandleEntityAttachmentCreate")
		defer span.End()

		_, parseSpan := startEntityCtrlSpan(spanCtx, "controller.V1.HandleEntityAttachmentCreate.parseForm")
		err := r.ParseMultipartForm(ctrl.maxUploadSize << 20)
		if err != nil {
			recordCtrlSpanError(parseSpan, err)
			parseSpan.End()
			recordCtrlSpanError(span, err)
			log.Err(err).Msg("failed to parse multipart form")
			return multipartFormError(err)
		}

		errs := validate.NewFieldErrors()

		file, _, err := r.FormFile("file")
		if err != nil {
			switch {
			case errors.Is(err, http.ErrMissingFile):
				log.Debug().Msg("file for attachment is missing")
				errs = errs.Append("file", "file is required")
			default:
				recordCtrlSpanError(parseSpan, err)
				parseSpan.End()
				recordCtrlSpanError(span, err)
				log.Err(err).Msg("failed to get file from form")
				return validate.NewRequestError(err, http.StatusInternalServerError)
			}
		}

		attachmentName := r.FormValue("name")
		if attachmentName == "" {
			log.Debug().Msg("failed to get name from form")
			errs = errs.Append("name", "name is required")
		}
		parseSpan.End()

		if !errs.Nil() {
			span.SetAttributes(attribute.String("response.status", "validation_error"))
			return server.JSON(w, http.StatusUnprocessableEntity, errs)
		}

		attachmentName = sanitizeAttachmentName(attachmentName)

		attachmentType := r.FormValue("type")
		if attachmentType == "" {
			ext := filepath.Ext(attachmentName)

			switch strings.ToLower(ext) {
			case ".jpg", ".jpeg", ".png", ".webp", ".gif", ".bmp", ".tiff", ".avif", ".ico", ".heic", ".jxl":
				attachmentType = attachment.TypePhoto.String()
			default:
				attachmentType = attachment.TypeAttachment.String()
			}
		}

		primary, err := strconv.ParseBool(r.FormValue("primary"))
		if err != nil {
			log.Debug().Msg("failed to parse primary from form")
			primary = false
		}

		id, err := ctrl.routeID(r)
		if err != nil {
			recordCtrlSpanError(span, err)
			return err
		}

		span.SetAttributes(
			attribute.String("entity.id", id.String()),
			attribute.String("attachment.name", attachmentName),
			attribute.String("attachment.type", attachmentType),
			attribute.Bool("attachment.primary", primary),
		)

		ctx := services.NewContext(spanCtx)
		span.SetAttributes(attribute.String("group.id", ctx.GID.String()))

		item, err := ctrl.svc.Entities.AttachmentAdd(
			ctx,
			id,
			attachmentName,
			attachment.Type(attachmentType),
			primary,
			file,
		)
		if err != nil {
			recordCtrlSpanError(span, err)
			log.Err(err).Msg("failed to add attachment")
			return validate.NewRequestError(err, http.StatusInternalServerError)
		}

		return server.JSON(w, http.StatusCreated, item)
	}
}

backend/app/api/routes.go (lines 210-245):

r.Get("/entities/{id}", chain.ToHandlerFunc(v1Ctrl.HandleEntityGet(), userMW...))
		r.Get("/entities/{id}/path", chain.ToHandlerFunc(v1Ctrl.HandleEntityFullPath(), userMW...))
		r.Put("/entities/{id}", chain.ToHandlerFunc(v1Ctrl.HandleEntityUpdate(), userMW...))
		r.Patch("/entities/{id}", chain.ToHandlerFunc(v1Ctrl.HandleEntityPatch(), userMW...))
		r.Delete("/entities/{id}", chain.ToHandlerFunc(v1Ctrl.HandleEntityDelete(), userMW...))
		r.Post("/entities/{id}/duplicate", chain.ToHandlerFunc(v1Ctrl.HandleEntityDuplicate(), userMW...))

		// Entity attachment endpoints
		r.Post("/entities/{id}/attachments", chain.ToHandlerFunc(v1Ctrl.HandleEntityAttachmentCreate(), userMW...))
		r.Post("/entities/{id}/attachments/external", chain.ToHandlerFunc(v1Ctrl.HandleEntityAttachmentExternalCreate(), userMW...))
		r.Put("/entities/{id}/attachments/{attachment_id}", chain.ToHandlerFunc(v1Ctrl.HandleEntityAttachmentUpdate(), userMW...))
		r.Delete("/entities/{id}/attachments/{attachment_id}", chain.ToHandlerFunc(v1Ctrl.HandleEntityAttachmentDelete(), userMW...))

		// Entity maintenance endpoints
		r.Get("/entities/{id}/maintenance", chain.ToHandlerFunc(v1Ctrl.HandleMaintenanceLogGet(), userMW...))
		r.Post("/entities/{id}/maintenance", chain.ToHandlerFunc(v1Ctrl.HandleMaintenanceEntryCreate(), userMW...))

		r.Get("/assets/{id}", chain.ToHandlerFunc(v1Ctrl.HandleAssetGet(), userMW...))

		// Entity Templates
		r.Get("/templates", chain.ToHandlerFunc(v1Ctrl.HandleEntityTemplatesGetAll(), userMW...))
		r.Post("/templates", chain.ToHandlerFunc(v1Ctrl.HandleEntityTemplatesCreate(), userMW...))
		r.Get("/templates/{id}", chain.ToHandlerFunc(v1Ctrl.HandleEntityTemplatesGet(), userMW...))
		r.Put("/templates/{id}", chain.ToHandlerFunc(v1Ctrl.HandleEntityTemplatesUpdate(), userMW...))
		r.Delete("/templates/{id}", chain.ToHandlerFunc(v1Ctrl.HandleEntityTemplatesDelete(), userMW...))
		r.Post("/templates/{id}/create-item", chain.ToHandlerFunc(v1Ctrl.HandleEntityTemplatesCreateItem(), userMW...))

		// Maintenance
		r.Get("/maintenance", chain.ToHandlerFunc(v1Ctrl.HandleMaintenanceGetAll(), userMW...))
		r.Put("/maintenance/{id}", chain.ToHandlerFunc(v1Ctrl.HandleMaintenanceEntryUpdate(), userMW...))
		r.Delete("/maintenance/{id}", chain.ToHandlerFunc(v1Ctrl.HandleMaintenanceEntryDelete(), userMW...))

		// Notifiers
		r.Get("/notifiers", chain.ToHandlerFunc(v1Ctrl.HandleGetUserNotifiers(), userMW...))
		r.Post("/notifiers", chain.ToHandlerFunc(v1Ctrl.HandleCreateNotifier(), userMW...))
		r.Put("/notifiers/{id}", chain.ToHandlerFunc(v1Ctrl.HandleUpdateNotifier(), userMW...))

		existing postgres migration that adds a FK:

		-- +goose Up
        ALTER TABLE tags ADD COLUMN icon varchar(255) NULL;
        ALTER TABLE tags ADD COLUMN tag_children uuid REFERENCES tags(id) ON DELETE SET NULL;


        existing sqlite3 migration that adds a FK:

        -- +goose Up
        ALTER TABLE tags ADD COLUMN icon varchar(255) NULL;
        ALTER TABLE tags ADD COLUMN tag_children uuid REFERENCES tags(id) ON DELETE SET NULL;




