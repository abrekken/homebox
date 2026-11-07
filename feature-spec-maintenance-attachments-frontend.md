# Task: Allow attachments on maintenance entries (frontend only)

You are working in Homebox, a home-inventory app. The frontend is Nuxt 4 + Vue 3
(`<script setup lang="ts">`), Tailwind, shadcn-vue components (`frontend/components/ui/`),
vue-i18n, and icons from `~icons/mdi/*`. Items are stored as `Entity`. An entity can have many
maintenance entries.

The backend for this feature is ALREADY DONE and merged on this branch. Maintenance entries
can now have file attachments. Your job is to expose that in the UI.

This prompt covers the FRONTEND ONLY. Do not modify anything under `backend/`.

## Backend contract (already implemented — do not change it)

- `POST /v1/maintenance/{id}/attachments` — multipart form: `file` (required), `name`
  (required, file name including extension), `type` (optional; if omitted the server picks
  `photo` for image extensions, otherwise `attachment`). Returns **201** with the updated
  `MaintenanceEntry` (including its `attachments`). Returns **413** if the file is larger than
  the server's upload limit.
- `DELETE /v1/maintenance/{id}/attachments/{attachment_id}` — returns **204**.
- Every maintenance entry response (`MaintenanceEntry`, and `MaintenanceEntryWithDetails`
  which embeds it) now has `attachments: ItemAttachment[]` — always an array, never null.
- Downloading/opening a maintenance attachment uses the EXISTING item attachment route:
  `GET /v1/entities/{itemId}/attachments/{attachmentId}`, where `itemId` is the item the
  maintenance entry belongs to. `MaintenanceEntryWithDetails.itemID` gives you that id.
- Maintenance attachments never appear in the item's own `EntityOut.attachments`, and they
  are never `primary`.
- Deleting a maintenance entry deletes its attachments server-side. The frontend does not
  need to do anything extra for that.

## Acceptance criteria

1. On the maintenance list (both the item's Maintenance tab, `pages/item/[id]/index/maintenance.vue`,
   and the global `pages/maintenance.vue` — both render `components/Maintenance/ListView.vue`),
   each entry card shows its attachments when it has any, each with "download" and
   "open in new tab" actions. Entries with no attachments show nothing extra.
2. When **editing** an existing entry (the `update` mode of `components/Maintenance/EditModal.vue`),
   the user sees the entry's current attachments, can upload one or more files, and can
   delete an attachment (with a confirmation prompt). These changes are saved immediately,
   independent of the modal's "Update" button, and the list behind the modal refreshes to
   show them.
3. When **creating** an entry (`create` mode) or **duplicating** one (`duplicate` mode), the
   user can pick one or more files before submitting. They appear in a pending list and can be
   removed before submit. After each entry is created, every pending file is uploaded to it.
   `create` mode can create the same entry on several items at once (`itemIds` is an array);
   in that case each created entry gets its own upload of every pending file.
4. Duplicating an entry does NOT copy the original entry's attachments (only the files the
   user picks in the modal are uploaded).
5. Upload errors show a toast: `items.toast.attachment_too_large` when the response status
   is 413, otherwise `items.toast.failed_upload_attachment`. A failed upload during create does
   not undo the created entry; the remaining files still upload and the dialog still closes.
6. The item page's existing attachments UI (view page and edit page) looks and behaves
   exactly as before.

## Design (follow this, do not invent a different one)

### Step 0 — regenerate the TypeScript types first

`frontend/lib/api/types/data-contracts.ts` is GENERATED and is currently stale: its
`MaintenanceEntry` interface does not yet have `attachments`. From the repo root run:

```
task typescript-types
```

Then confirm `MaintenanceEntry` in `data-contracts.ts` now contains
`attachments: ItemAttachment[];`. If it does not, STOP and report it — do not hand-edit
`data-contracts.ts`, and do not work around it with casts. The swagger source
(`backend/app/api/static/docs/swagger.json`) is already up to date.

### API client — `frontend/lib/api/classes/maintenance.ts`

Add two methods to `MaintenanceAPI`, modeled on `AttachmentsAPI.add` / `AttachmentsAPI.delete`
in `frontend/lib/api/classes/items.ts`:

```ts
addAttachment(id: string, file: File | Blob, filename: string, type: AttachmentTypes | null = null) {
  // FormData: "file", "name", and "type" only if provided. No "primary" field.
  // POST route(`/maintenance/${id}/attachments`), response type MaintenanceEntry
}

deleteAttachment(id: string, attachmentId: string) {
  // DELETE route(`/maintenance/${id}/attachments/${attachmentId}`), response void
}
```

Import `AttachmentTypes` from `../types/non-generated`. The UI calls `addAttachment` with
`type = null` so the server auto-detects photo vs attachment; do not add a type picker.

### Reusable list — `frontend/components/Item/AttachmentsList.vue`

This component already renders a list of `ItemAttachment` with download / open-in-new-tab
links built from `itemId`. Reuse it for maintenance attachments. Extend it backwards-compatibly:

- Add an optional prop `deletable: Boolean` (default `false`).
- Declare an emit `delete` with payload `attachmentId: string`.
- When `deletable` is true, render one extra icon button per row (both for external-link rows
  and file rows) using `MdiDelete` from `~icons/mdi/delete`, `buttonVariants({ size: 'icon', variant: 'destructive' })`
  or the `Button` component with `variant="destructive" size="icon"`, wrapped in a Tooltip
  like the existing buttons, tooltip text `$t("maintenance.list.delete")`. Clicking it emits
  `delete` with the attachment id. The component does NOT call the API or confirm itself.
- The existing caller (`pages/item/[id]/index.vue`) passes no `deletable` prop, so it is unchanged.

### List view — `frontend/components/Maintenance/ListView.vue`

- In each entry card (`BaseCard v-for="e in maintenanceDataList"`), between the description
  block and the button group, render:

  ```vue
  <div v-if="e.attachments?.length" class="px-6 pb-4">
    <ItemAttachmentsList :attachments="e.attachments" :item-id="e.itemID" />
  </div>
  ```

  (import as `ItemAttachmentsList` from `~/components/Item/AttachmentsList.vue`, the same name
  the item page uses). Read-only here — attachment management lives in the edit modal.
- Both data sources (`api.items.maintenance.getLog` and `api.maintenance.getAll`) return
  `MaintenanceEntryWithDetails[]`, so `e.itemID` is always present. Type `maintenanceDataList`
  accordingly if needed so `e.itemID` type-checks without `as` casts.
- `ListView.vue` already renders `<MaintenanceEditModal ... @changed="refreshList" />`, but
  the modal never emits `changed`. The modal will emit it (see below); no other ListView
  change is needed for refreshing.

### Edit modal — `frontend/components/Maintenance/EditModal.vue`

State to add:

```ts
const emit = defineEmits<{ changed: [] }>();

const attachments = ref<ItemAttachment[]>([]);   // existing attachments (update mode only)
const attachmentItemId = ref<string | null>(null); // item id for download URLs (update mode)
const pendingFiles = ref<File[]>([]);            // create/duplicate mode only
const uploading = ref(false);
const fileInput = ref<HTMLInputElement>();
```

In `registerOpenDialogCallback`:

- every mode: reset `pendingFiles.value = []` and `uploading.value = false`.
- `update`: `attachments.value = [...(params.maintenanceEntry.attachments ?? [])]`;
  `attachmentItemId.value = "itemID" in params.maintenanceEntry ? params.maintenanceEntry.itemID : null`.
- `create` / `duplicate`: `attachments.value = []`, `attachmentItemId.value = null`.
  Do not copy `params.maintenanceEntry.attachments` in `duplicate`.

UI: add an "Attachments" section inside the `<form>`, after the cost field and before
`DialogFooter`:

- A label `$t("maintenance.modal.attachments")`.
- An "add files" button (`type="button"`, so it does not submit the form) with icon
  `MdiPaperclip` and text `$t("maintenance.modal.add_attachments")` that clicks a hidden
  `<input ref="fileInput" type="file" multiple hidden @change="onFilesSelected" />` — same
  hidden-input pattern as `clickUpload` / `refAttachmentInput` in `pages/item/[id]/index/edit.vue`.
  Reset the input's `value` to `""` after reading it so selecting the same file twice works.
- **update mode** (`entry.id` set):
  - Show `$t("items.changes_persisted_immediately")` as a small hint (`class="text-xs"`).
  - `onFilesSelected` uploads each selected file in sequence with
    `api.maintenance.addAttachment(entry.id, file, file.name, null)`. On success replace
    `attachments.value` with `data.attachments`, toast `items.toast.attachment_uploaded`,
    and `emit("changed")`. On error toast as in acceptance criterion 5 and continue with the
    next file.
  - Render the list with
    `<ItemAttachmentsList v-if="attachments.length && attachmentItemId" :attachments="attachments" :item-id="attachmentItemId" deletable @delete="removeAttachment" />`.
  - `removeAttachment(id)`: `confirm.open(t("items.delete_attachment_confirm"))` (use
    `useConfirm()` as `ListView.vue` does); if not canceled call
    `api.maintenance.deleteAttachment(entry.id, id)`; on error toast
    `items.toast.failed_delete_attachment`; on success remove it from `attachments.value`,
    toast `items.toast.attachment_deleted`, and `emit("changed")`.
- **create / duplicate mode** (`entry.id` null):
  - Show `$t("maintenance.modal.pending_attachments_hint")` as a small hint.
  - `onFilesSelected` appends the files to `pendingFiles.value` (no upload yet).
  - Render `pendingFiles` as a simple list (file name + a `type="button"` icon button with
    `MdiClose` that removes it from the array). Match the look of `AttachmentsList.vue`
    (`ul.divide-y.rounded-md.border`, `li` with `py-3 pl-3 pr-4 text-sm`).

Submit changes:

- `createEntry()`: inside the existing per-item callback, after a successful
  `api.items.maintenance.create(...)`, upload every file in `pendingFiles.value` to `data.id`
  in sequence via `api.maintenance.addAttachment`, toasting each failure as in criterion 5.
  Keep `Promise.allSettled` across items. Set `uploading.value = true` before and `false` after.
  Do not toast success for each file during create (avoid N×M toasts).
- `editEntry()`: unchanged (attachments were already saved as they were added/removed).
- Disable the submit button and the add-files button while `uploading` is true.

Imports needed in the modal: `ItemAttachment` type from `~~/lib/api/types/data-contracts`,
`ItemAttachmentsList` from `~/components/Item/AttachmentsList.vue`, `MdiPaperclip`
(`~icons/mdi/paperclip`), `MdiClose` (`~icons/mdi/close`), `Label` from `@/components/ui/label`
if you use it for the section label.

### i18n — `frontend/locales/en.json` ONLY

Add under `maintenance.modal`, keeping the keys in alphabetical order like the rest of the file:

```json
"add_attachments": "Add Attachments",
"attachments": "Attachments",
"pending_attachments_hint": "Files will be uploaded when the entry is saved",
```

Reuse the existing `items.toast.*`, `items.delete_attachment_confirm`,
`items.changes_persisted_immediately` and `maintenance.list.delete` keys; do not duplicate them.
Do NOT edit any other locale file — other languages are translated through Weblate.

## Files to change

1. `frontend/lib/api/types/data-contracts.ts` — regenerated only (Step 0), never hand-edited.
2. `frontend/lib/api/classes/maintenance.ts` — `addAttachment`, `deleteAttachment`.
3. `frontend/components/Item/AttachmentsList.vue` — optional `deletable` prop + `delete` emit.
4. `frontend/components/Maintenance/ListView.vue` — show attachments on each card.
5. `frontend/components/Maintenance/EditModal.vue` — attachments section, pending uploads, `changed` emit.
6. `frontend/locales/en.json` — three new keys.
7. `frontend/lib/api/__test__/user/items.test.ts` — new test (see Tests).

No other files should need changes. If you believe one does, say why before changing it.

## Constraints

- Do not modify anything under `backend/` or `frontend/components/ui/` (generated shadcn components).
- Follow the existing code style: `<script setup lang="ts">`, `useUserApi()`, `useI18n()`,
  `toast` from `@/components/ui/sonner`, API calls destructured as `{ data, error, status }`.
- No `any`, no `as` casts to paper over missing types — if a type is missing, Step 0 was not done.
- No new dependencies.
- Out of scope: thumbnails / image previews for maintenance photos, an attachment type picker,
  renaming or editing attachment metadata, drag-and-drop upload, external-link attachments,
  translations other than `en.json`, and the existing `duplicate` button on the global
  maintenance page passing `itemId: props.currentItemId!` (undefined there) — leave it as is.

## Tests

Add a test to `frontend/lib/api/__test__/user/items.test.ts`, inside the existing
`describe` block, following the pattern of
`"users should be able to create and few maintenance logs for an item"`:

1. Create a location (`useLocation`) and an item in it.
2. Create one maintenance entry with `api.items.maintenance.create`; expect 201 and
   `data.attachments` to equal `[]`.
3. `api.maintenance.addAttachment(entry.id, new Blob(["receipt"], { type: "text/plain" }), "receipt.txt")`;
   expect status 201, `data.attachments` length 1, title `"receipt.txt"`, `primary` false.
4. `api.items.maintenance.getLog(item.id)`: the entry has 1 attachment.
5. `api.items.get(item.id)`: `data.attachments` does NOT contain that attachment id.
6. `api.maintenance.deleteAttachment(entry.id, attachmentId)`; expect 204. `getLog` again: 0 attachments.
7. Call the location `cleanup()`.

These are integration tests: they need the backend API running on `http://127.0.0.1:7745`.
Start it in one terminal with `task go:run`, then in `frontend/` run `pnpm run test:local`.

## Definition of done

From `frontend/`, all of these pass:

```
pnpm run lint:ci
pnpm run typecheck
pnpm run test:local     # with the API running, see Tests
```

Then a manual check with `task go:run` + `task ui:dev`:
create an item → Maintenance tab → New entry with two pending files → both show on the card
and download correctly → Edit the entry, add a third file and delete one → card updates
without reloading → the item's own attachments section does not show any of them.

## Output format

For each file, give the full path and either the complete new file or a unified diff.
List any commands I need to run, in order. If something in this spec conflicts with what
you see in the code, say so instead of guessing.
