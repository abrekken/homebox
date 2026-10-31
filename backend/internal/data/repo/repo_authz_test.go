package repo

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/sysadminsmedia/homebox/backend/internal/data/ent"
	"github.com/sysadminsmedia/homebox/backend/internal/data/ent/attachment"
	"github.com/sysadminsmedia/homebox/backend/internal/data/types"
)

func nowDate() types.Date {
	return types.DateFromTime(time.Now())
}

// makeForeignGroup creates a second tenant group with its own item entity type
// and tag, returning the group ID, a tag ID, and an entity type ID that all
// belong to that foreign group. Callers use these IDs to assert that the
// primary tGroup write paths refuse to attach foreign-tenant references.
func makeForeignGroup(t *testing.T) (gid uuid.UUID, foreignTagID uuid.UUID, foreignTypeID uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	other, err := tRepos.Groups.GroupCreate(ctx, "authz-foreign-"+uuid.NewString(), uuid.Nil)
	require.NoError(t, err)

	otherType, err := tRepos.EntityTypes.GetDefault(ctx, other.ID, false)
	require.NoError(t, err)

	otherTag, err := tRepos.Tags.Create(ctx, other.ID, TagCreate{Name: "foreign-tag-" + uuid.NewString()})
	require.NoError(t, err)

	return other.ID, otherTag.ID, otherType.ID
}

func Test_EntityCreate_RejectsForeignParent(t *testing.T) {
	itemET := useItemEntityType(t)

	foreignGID, _, foreignTypeID := makeForeignGroup(t)
	foreignParent, err := tRepos.Entities.Create(context.Background(), foreignGID, EntityCreate{
		Name:         "foreign-parent",
		EntityTypeID: foreignTypeID,
	})
	require.NoError(t, err)

	_, err = tRepos.Entities.Create(context.Background(), tGroup.ID, EntityCreate{
		Name:         "victim",
		EntityTypeID: itemET.ID,
		ParentID:     foreignParent.ID,
	})
	require.Error(t, err)
	assert.True(t, ent.IsNotFound(err), "expected NotFound for cross-group ParentID, got %T: %v", err, err)
}

func Test_EntityCreate_RejectsForeignEntityType(t *testing.T) {
	_, _, foreignTypeID := makeForeignGroup(t)

	_, err := tRepos.Entities.Create(context.Background(), tGroup.ID, EntityCreate{
		Name:         "victim",
		EntityTypeID: foreignTypeID,
	})
	require.Error(t, err)
	assert.True(t, ent.IsNotFound(err), "expected NotFound for cross-group EntityTypeID, got %T: %v", err, err)
}

func Test_EntityCreate_RejectsForeignTag(t *testing.T) {
	itemET := useItemEntityType(t)
	_, foreignTagID, _ := makeForeignGroup(t)

	_, err := tRepos.Entities.Create(context.Background(), tGroup.ID, EntityCreate{
		Name:         "victim",
		EntityTypeID: itemET.ID,
		TagIDs:       []uuid.UUID{foreignTagID},
	})
	require.Error(t, err)
	assert.True(t, ent.IsNotFound(err), "expected NotFound for cross-group TagIDs, got %T: %v", err, err)
}

func Test_EntityPatch_RejectsForeignTag(t *testing.T) {
	itemET := useItemEntityType(t)
	_, foreignTagID, _ := makeForeignGroup(t)

	// Create a victim entity inside tGroup first.
	victim, err := tRepos.Entities.Create(context.Background(), tGroup.ID, EntityCreate{
		Name:         "victim-patch",
		EntityTypeID: itemET.ID,
	})
	require.NoError(t, err)

	err = tRepos.Entities.Patch(context.Background(), tGroup.ID, victim.ID, EntityPatch{
		TagIDs: []uuid.UUID{foreignTagID},
	})
	require.Error(t, err)
	assert.True(t, ent.IsNotFound(err), "expected NotFound for cross-group tag in Patch, got %T: %v", err, err)
}

func Test_MaintEntryCreate_RejectsForeignItem(t *testing.T) {
	itemET := useItemEntityType(t)
	foreignGID, _, _ := makeForeignGroup(t)
	foreignType, err := tRepos.EntityTypes.GetDefault(context.Background(), foreignGID, false)
	require.NoError(t, err)

	foreignItem, err := tRepos.Entities.Create(context.Background(), foreignGID, EntityCreate{
		Name:         "foreign-item",
		EntityTypeID: foreignType.ID,
	})
	require.NoError(t, err)

	// Caller is in tGroup but supplies a foreign item ID — must be rejected
	// before any maintenance row is written against B's item.
	_, err = tRepos.MaintEntry.Create(context.Background(), tGroup.ID, foreignItem.ID, MaintenanceEntryCreate{
		CompletedDate: nowDate(),
		Name:          "should-be-rejected",
	})
	require.Error(t, err)
	assert.True(t, ent.IsNotFound(err), "expected NotFound for cross-group MaintEntry.Create, got %T: %v", err, err)

	// Confirm the legitimate tGroup item path still works.
	ownItem, err := tRepos.Entities.Create(context.Background(), tGroup.ID, EntityCreate{
		Name:         "own-item",
		EntityTypeID: itemET.ID,
	})
	require.NoError(t, err)
	_, err = tRepos.MaintEntry.Create(context.Background(), tGroup.ID, ownItem.ID, MaintenanceEntryCreate{
		CompletedDate: nowDate(),
		Name:          "legit",
	})
	require.NoError(t, err)
}

func Test_MaintEntryAttachment_RejectsForeignEntry(t *testing.T) {
	ctx := context.Background()

	foreignGID, _, foreignTypeID := makeForeignGroup(t)
	foreignItem, err := tRepos.Entities.Create(ctx, foreignGID, EntityCreate{
		Name:         "foreign-item-att",
		EntityTypeID: foreignTypeID,
	})
	require.NoError(t, err)
	foreignEntry, err := tRepos.MaintEntry.Create(ctx, foreignGID, foreignItem.ID, MaintenanceEntryCreate{
		CompletedDate: nowDate(),
		Name:          "foreign-entry",
	})
	require.NoError(t, err)

	// Caller is in tGroup but supplies a foreign maintenance entry ID — the
	// attachment must not be created against B's entry.
	_, err = tRepos.Attachments.CreateForMaintenance(ctx, tGroup.ID, foreignEntry.ID, ItemCreateAttachment{
		Title:   "should-be-rejected.pdf",
		Content: strings.NewReader("nope"),
	}, attachment.TypeAttachment)
	require.Error(t, err)
	assert.True(t, ent.IsNotFound(err), "expected NotFound for cross-group CreateForMaintenance, got %T: %v", err, err)

	// Same for the scoped delete path.
	err = tRepos.Attachments.DeleteForMaintenance(ctx, tGroup.ID, foreignEntry.ID, uuid.New())
	require.Error(t, err)
	assert.True(t, ent.IsNotFound(err), "expected NotFound for cross-group DeleteForMaintenance, got %T: %v", err, err)
}

func Test_MaintEntryDelete_CascadesAttachments(t *testing.T) {
	ctx := context.Background()
	itemET := useItemEntityType(t)

	item, err := tRepos.Entities.Create(ctx, tGroup.ID, EntityCreate{
		Name:         "cascade-item",
		EntityTypeID: itemET.ID,
	})
	require.NoError(t, err)

	entry, err := tRepos.MaintEntry.Create(ctx, tGroup.ID, item.ID, MaintenanceEntryCreate{
		CompletedDate: nowDate(),
		Name:          "cascade-entry",
	})
	require.NoError(t, err)

	att, err := tRepos.Attachments.CreateForMaintenance(ctx, tGroup.ID, entry.ID, ItemCreateAttachment{
		Title:   "cascade.pdf",
		Content: strings.NewReader("cascade"),
	}, attachment.TypeAttachment)
	require.NoError(t, err)

	require.NoError(t, tRepos.MaintEntry.Delete(ctx, tGroup.ID, entry.ID))

	_, err = tRepos.Attachments.Get(ctx, tGroup.ID, att.ID)
	require.Error(t, err, "attachment should be deleted with its maintenance entry")
}

func Test_EntityTemplateCreate_RejectsForeignDefaultLocation(t *testing.T) {
	foreignGID, _, _ := makeForeignGroup(t)
	foreignContainerType, err := tRepos.EntityTypes.GetDefault(context.Background(), foreignGID, true)
	require.NoError(t, err)
	foreignLoc, err := tRepos.Entities.Create(context.Background(), foreignGID, EntityCreate{
		Name:         "foreign-loc",
		EntityTypeID: foreignContainerType.ID,
	})
	require.NoError(t, err)

	_, err = tRepos.EntityTemplates.Create(context.Background(), tGroup.ID, EntityTemplateCreate{
		Name:              "victim-template",
		DefaultLocationID: foreignLoc.ID,
	})
	require.Error(t, err)
	assert.True(t, ent.IsNotFound(err), "expected NotFound for cross-group DefaultLocationID, got %T: %v", err, err)
}
