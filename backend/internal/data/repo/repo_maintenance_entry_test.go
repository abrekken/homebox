package repo

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/sysadminsmedia/homebox/backend/internal/data/ent/attachment"
	"github.com/sysadminsmedia/homebox/backend/internal/data/types"
)

// get the previous month from the current month, accounts for errors when run
// near the beginning or end of the month/year
func getPrevMonth(now time.Time) time.Time {
	t := now.AddDate(0, -1, 0)

	// avoid infinite loop
	max := 15
	for t.Month() == now.Month() {
		t = t.AddDate(0, 0, -1)

		max--
		if max == 0 {
			panic("max exceeded")
		}
	}

	return t
}

func TestMaintenanceEntryRepository_GetLog(t *testing.T) {
	item := useEntities(t, 1)[0]

	// Create 11 maintenance entries for the item
	created := make([]MaintenanceEntryCreate, 11)

	thisMonth := time.Now()
	lastMonth := getPrevMonth(thisMonth)

	for i := 0; i < 10; i++ {
		dt := lastMonth
		if i%2 == 0 {
			dt = thisMonth
		}

		created[i] = MaintenanceEntryCreate{
			CompletedDate: types.DateFromTime(dt),
			Name:          "Maintenance",
			Description:   "Maintenance description",
			Cost:          10,
		}
	}

	// Add an entry completed in the future
	created[10] = MaintenanceEntryCreate{
		CompletedDate: types.DateFromTime(time.Now().AddDate(0, 0, 1)),
		Name:          "Maintenance",
		Description:   "Maintenance description",
		Cost:          10,
	}

	for _, entry := range created {
		_, err := tRepos.MaintEntry.Create(context.Background(), tGroup.ID, item.ID, entry)
		if err != nil {
			t.Fatalf("failed to create maintenance entry: %v", err)
		}
	}

	// Get the log for the item
	log, err := tRepos.MaintEntry.GetMaintenanceByItemID(context.Background(), tGroup.ID, item.ID, MaintenanceFilters{Status: MaintenanceFilterStatusCompleted})
	if err != nil {
		t.Fatalf("failed to get maintenance log: %v", err)
	}

	assert.Len(t, log, 10)

	for _, entry := range log {
		err := tRepos.MaintEntry.Delete(context.Background(), tGroup.ID, entry.ID)
		require.NoError(t, err)
	}
}

func TestMaintenanceEntryAttachments(t *testing.T) {
	ctx := context.Background()
	item := useEntities(t, 1)[0]

	entry, err := tRepos.MaintEntry.Create(ctx, tGroup.ID, item.ID, MaintenanceEntryCreate{
		CompletedDate: types.DateFromTime(time.Now()),
		Name:          "Attachment entry",
	})
	require.NoError(t, err)
	assert.NotNil(t, entry.Attachments, "attachments must never be null")
	assert.Empty(t, entry.Attachments)

	att, err := tRepos.Attachments.CreateForMaintenance(
		ctx,
		tGroup.ID,
		entry.ID,
		ItemCreateAttachment{Title: "receipt.pdf", Content: strings.NewReader("receipt content")},
		attachment.TypeAttachment,
	)
	require.NoError(t, err)
	assert.False(t, att.Primary, "maintenance attachments must never be primary")

	// The attachment is linked to the item so the existing entity attachment
	// download route keeps working.
	withEntity, err := tRepos.Attachments.Get(ctx, tGroup.ID, att.ID)
	require.NoError(t, err)
	assert.Equal(t, item.ID, withEntity.Edges.Entity.ID)

	// Entry responses include the attachment.
	got, err := tRepos.MaintEntry.Get(ctx, tGroup.ID, entry.ID)
	require.NoError(t, err)
	require.Len(t, got.Attachments, 1)
	assert.Equal(t, att.ID, got.Attachments[0].ID)
	assert.False(t, got.Attachments[0].Primary)

	// Update keeps attachments in the response.
	updated, err := tRepos.MaintEntry.Update(ctx, tGroup.ID, entry.ID, MaintenanceEntryUpdate{
		CompletedDate: types.DateFromTime(time.Now()),
		Name:          "Attachment entry updated",
	})
	require.NoError(t, err)
	require.Len(t, updated.Attachments, 1)

	// Log queries include attachments.
	log, err := tRepos.MaintEntry.GetMaintenanceByItemID(ctx, tGroup.ID, item.ID, MaintenanceFilters{Status: MaintenanceFilterStatusCompleted})
	require.NoError(t, err)
	require.Len(t, log, 1)
	assert.Len(t, log[0].Attachments, 1)

	// The item's own attachment list must not include maintenance attachments.
	entityOut, err := tRepos.Entities.GetOne(ctx, item.ID)
	require.NoError(t, err)
	for _, a := range entityOut.Attachments {
		assert.NotEqual(t, att.ID, a.ID, "maintenance attachment leaked into EntityOut.Attachments")
	}

	// Deleting the entry deletes its attachments.
	err = tRepos.MaintEntry.Delete(ctx, tGroup.ID, entry.ID)
	require.NoError(t, err)

	_, err = tRepos.Attachments.Get(ctx, tGroup.ID, att.ID)
	require.Error(t, err, "attachment should be deleted with its maintenance entry")
}

func TestMaintenanceEntryAttachments_NeverPrimary(t *testing.T) {
	ctx := context.Background()
	item := useEntities(t, 1)[0]

	entry, err := tRepos.MaintEntry.Create(ctx, tGroup.ID, item.ID, MaintenanceEntryCreate{
		CompletedDate: types.DateFromTime(time.Now()),
		Name:          "Primary photo entry",
	})
	require.NoError(t, err)

	maintPhoto, err := tRepos.Attachments.CreateForMaintenance(ctx, tGroup.ID, entry.ID,
		ItemCreateAttachment{Title: "receipt.jpg", Content: strings.NewReader("receipt photo")},
		attachment.TypePhoto,
	)
	require.NoError(t, err)
	assert.False(t, maintPhoto.Primary)

	// SetPrimaryPhotos must not promote a maintenance photo.
	_, err = tRepos.Entities.SetPrimaryPhotos(ctx, tGroup.ID)
	require.NoError(t, err)
	got, err := tRepos.Attachments.Get(ctx, tGroup.ID, maintPhoto.ID)
	require.NoError(t, err)
	assert.False(t, got.Primary, "SetPrimaryPhotos promoted a maintenance photo")

	// Updating through the item attachment route cannot make it primary.
	updated, err := tRepos.Attachments.Update(ctx, tGroup.ID, maintPhoto.ID, &ItemAttachmentUpdate{
		Type:    attachment.TypePhoto.String(),
		Primary: true,
	})
	require.NoError(t, err)
	assert.False(t, updated.Primary, "maintenance photo was set primary via Update")

	// The item's first real photo still becomes primary automatically.
	itemPhoto, err := tRepos.Attachments.Create(ctx, item.ID,
		ItemCreateAttachment{Title: "item.jpg", Content: strings.NewReader("item photo")},
		attachment.TypePhoto, false,
	)
	require.NoError(t, err)
	assert.True(t, itemPhoto.Primary, "maintenance photo blocked auto-primary on the item's first photo")
}
