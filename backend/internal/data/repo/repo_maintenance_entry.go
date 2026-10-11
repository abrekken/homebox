package repo

import (
	"context"
	"errors"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
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
	db          *ent.Client
	attachments *AttachmentRepo
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
		ID            uuid.UUID        `json:"id"`
		CompletedDate types.Date       `json:"completedDate"`
		ScheduledDate types.Date       `json:"scheduledDate"`
		Name          string           `json:"name"`
		Description   string           `json:"description"`
		Cost          float64          `json:"cost,string"`
		Attachments   []ItemAttachment `json:"attachments"`
	}
)

var (
	mapMaintenanceEntryErr  = mapTErrFunc(mapMaintenanceEntry)
	mapEachMaintenanceEntry = mapTEachFunc(mapMaintenanceEntry)
)

func mapMaintenanceEntry(entry *ent.MaintenanceEntry) MaintenanceEntry {
	attachments := []ItemAttachment{}
	if entry.Edges.Attachments != nil {
		attachments = mapEach(entry.Edges.Attachments, ToItemAttachment)
	}

	return MaintenanceEntry{
		ID:            entry.ID,
		CompletedDate: types.DateFromDBTime(entry.Date),
		ScheduledDate: types.DateFromDBTime(entry.ScheduledDate),
		Name:          entry.Name,
		Description:   entry.Description,
		Cost:          entry.Cost,
		Attachments:   attachments,
	}
}

func (r *MaintenanceEntryRepository) Get(ctx context.Context, gid uuid.UUID, id uuid.UUID) (MaintenanceEntry, error) {
	entry, err := r.db.MaintenanceEntry.Query().
		Where(
			maintenanceentry.ID(id),
			maintenanceentry.HasEntityWith(entity.HasGroupWith(group.ID(gid))),
		).
		WithAttachments().
		Only(ctx)

	return mapMaintenanceEntryErr(entry, err)
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
		WithAttachments().
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

	_, err = r.db.MaintenanceEntry.UpdateOneID(id).
		SetDate(input.CompletedDate.Time()).
		SetScheduledDate(input.ScheduledDate.Time()).
		SetName(input.Name).
		SetDescription(input.Description).
		SetCost(input.Cost).
		Save(ctx)
	if err != nil {
		return MaintenanceEntry{}, err
	}

	// Re-fetch so the returned entry carries its attachments.
	return r.Get(ctx, gid, id)
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
	entries, err := query.WithEntity().WithAttachments().All(ctx)

	if err != nil {
		return []MaintenanceEntryWithDetails{}, err
	}

	return mapEachMaintenanceEntryWithDetails(entries), nil
}

func (r *MaintenanceEntryRepository) Delete(ctx context.Context, gid uuid.UUID, id uuid.UUID) error {
	entry, err := r.db.MaintenanceEntry.Query().
		Where(
			maintenanceentry.ID(id),
			maintenanceentry.HasEntityWith(entity.HasGroupWith(group.ID(gid))),
		).
		WithAttachments().
		Only(ctx)
	if err != nil {
		return err
	}

	// Delete the entry's attachments (including their stored files) before the
	// entry itself; the FK to maintenance_entries is ON DELETE SET NULL, so
	// deleting the entry first would orphan the attachment rows and files.
	for _, att := range entry.Edges.Attachments {
		if err := r.attachments.Delete(ctx, gid, att.ID); err != nil {
			log.Err(err).Str("attachment_id", att.ID.String()).
				Msg("failed to delete maintenance entry attachment")
			return err
		}
	}

	return r.db.MaintenanceEntry.DeleteOneID(id).Exec(ctx)
}
