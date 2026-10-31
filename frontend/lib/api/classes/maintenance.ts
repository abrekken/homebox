import { BaseAPI, route } from "../base";
import type {
  MaintenanceEntry,
  MaintenanceEntryUpdate,
  MaintenanceEntryWithDetails,
  MaintenanceFilterStatus,
} from "../types/data-contracts";
import type { AttachmentTypes } from "../types/non-generated";

export interface MaintenanceFilters {
  status?: MaintenanceFilterStatus;
}

export class MaintenanceAPI extends BaseAPI {
  getAll(filters: MaintenanceFilters) {
    return this.http.get<MaintenanceEntryWithDetails[]>({
      url: route(`/maintenance`, { status: filters.status?.toString() }),
    });
  }

  delete(id: string) {
    return this.http.delete<void>({ url: route(`/maintenance/${id}`) });
  }

  update(id: string, data: MaintenanceEntryUpdate) {
    return this.http.put<MaintenanceEntryUpdate, MaintenanceEntry>({
      url: route(`/maintenance/${id}`),
      body: data,
    });
  }

  addAttachment(id: string, file: File | Blob, filename: string, type: AttachmentTypes | null = null) {
    const formData = new FormData();
    formData.append("file", file);
    formData.append("name", filename);
    if (type) {
      formData.append("type", type);
    }

    return this.http.post<FormData, MaintenanceEntry>({
      url: route(`/maintenance/${id}/attachments`),
      data: formData,
    });
  }

  deleteAttachment(id: string, attachmentId: string) {
    return this.http.delete<void>({ url: route(`/maintenance/${id}/attachments/${attachmentId}`) });
  }
}
