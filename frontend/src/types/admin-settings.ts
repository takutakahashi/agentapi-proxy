export type AdminSettingsSections = Record<string, Record<string, unknown>>

export interface AdminSettingsDocument {
  schema_version: number
  version: number
  sections: AdminSettingsSections
  secret_configured: Record<string, boolean>
  updated_at?: string
}

export interface AdminSettingsVersion {
  version: number
  updated_at: string
}

export interface AdminSettingsVersionsResponse {
  versions: AdminSettingsVersion[]
}

export interface UpdateAdminSettingsRequest {
  base_version: number
  sections: AdminSettingsSections
}
