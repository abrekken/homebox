package v1

import (
	"errors"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/hay-kot/httpkit/errchain"
	"github.com/hay-kot/httpkit/server"
	"github.com/rs/zerolog/log"
	"github.com/sysadminsmedia/homebox/backend/internal/core/services"
	"github.com/sysadminsmedia/homebox/backend/internal/data/ent"
	"github.com/sysadminsmedia/homebox/backend/internal/data/ent/attachment"
	"github.com/sysadminsmedia/homebox/backend/internal/data/repo"
	"github.com/sysadminsmedia/homebox/backend/internal/sys/validate"
	"go.opentelemetry.io/otel/attribute"
)

// HandleMaintenanceEntryAttachmentCreate godoc
//
//	@Summary	Create Maintenance Entry Attachment
//	@Tags		Maintenance Attachments
//	@Accept		multipart/form-data
//	@Produce	json
//	@Param		id		path		string	true	"Maintenance Entry ID"
//	@Param		file	formData	file	true	"File attachment"
//	@Param		name	formData	string	true	"name of the file including extension"
//	@Param		type	formData	string	false	"Type of file"
//	@Success	201		{object}	repo.MaintenanceEntry
//	@Failure	404		{object}	validate.ErrorResponse
//	@Failure	422		{object}	validate.ErrorResponse
//	@Router		/v1/maintenance/{id}/attachments [POST]
//	@Security	Bearer
func (ctrl *V1Controller) HandleMaintenanceEntryAttachmentCreate() errchain.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		spanCtx, span := startEntityCtrlSpan(r.Context(), "controller.V1.HandleMaintenanceEntryAttachmentCreate")
		defer span.End()

		_, parseSpan := startEntityCtrlSpan(spanCtx, "controller.V1.HandleMaintenanceEntryAttachmentCreate.parseForm")
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

		id, err := ctrl.routeID(r)
		if err != nil {
			recordCtrlSpanError(span, err)
			return err
		}

		span.SetAttributes(
			attribute.String("maintenance_entry.id", id.String()),
			attribute.String("attachment.name", attachmentName),
			attribute.String("attachment.type", attachmentType),
		)

		ctx := services.NewContext(spanCtx)
		span.SetAttributes(attribute.String("group.id", ctx.GID.String()))

		_, err = ctrl.repo.Attachments.CreateForMaintenance(
			spanCtx,
			ctx.GID,
			id,
			repo.ItemCreateAttachment{
				Title:   attachmentName,
				Content: file,
			},
			attachment.Type(attachmentType),
		)
		if err != nil {
			recordCtrlSpanError(span, err)
			if ent.IsNotFound(err) {
				return err
			}
			log.Err(err).Msg("failed to add maintenance entry attachment")
			return validate.NewRequestError(err, http.StatusInternalServerError)
		}

		entry, err := ctrl.repo.MaintEntry.Get(spanCtx, ctx.GID, id)
		if err != nil {
			recordCtrlSpanError(span, err)
			if ent.IsNotFound(err) {
				return err
			}
			log.Err(err).Msg("failed to get maintenance entry after attachment create")
			return validate.NewRequestError(err, http.StatusInternalServerError)
		}

		return server.JSON(w, http.StatusCreated, entry)
	}
}

// HandleMaintenanceEntryAttachmentDelete godoc
//
//	@Summary	Delete Maintenance Entry Attachment
//	@Tags		Maintenance Attachments
//	@Param		id				path	string	true	"Maintenance Entry ID"
//	@Param		attachment_id	path	string	true	"Attachment ID"
//	@Success	204
//	@Failure	404	{object}	validate.ErrorResponse
//	@Router		/v1/maintenance/{id}/attachments/{attachment_id} [DELETE]
//	@Security	Bearer
func (ctrl *V1Controller) HandleMaintenanceEntryAttachmentDelete() errchain.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		spanCtx, span := startEntityCtrlSpan(r.Context(), "controller.V1.HandleMaintenanceEntryAttachmentDelete")
		defer span.End()

		id, err := ctrl.routeID(r)
		if err != nil {
			recordCtrlSpanError(span, err)
			return err
		}

		attachmentID, err := ctrl.routeUUID(r, "attachment_id")
		if err != nil {
			recordCtrlSpanError(span, err)
			return err
		}

		span.SetAttributes(
			attribute.String("maintenance_entry.id", id.String()),
			attribute.String("attachment.id", attachmentID.String()),
		)

		ctx := services.NewContext(spanCtx)
		span.SetAttributes(attribute.String("group.id", ctx.GID.String()))

		err = ctrl.repo.Attachments.DeleteForMaintenance(spanCtx, ctx.GID, id, attachmentID)
		if err != nil {
			recordCtrlSpanError(span, err)
			if ent.IsNotFound(err) {
				return err
			}
			log.Err(err).Msg("failed to delete maintenance entry attachment")
			return validate.NewRequestError(err, http.StatusInternalServerError)
		}

		return server.JSON(w, http.StatusNoContent, nil)
	}
}
