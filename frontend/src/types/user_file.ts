export interface UserFile {
  id: string
  name: string
  path: string
  content: string
  permissions: string
  created_at: string
  updated_at: string
}

export interface FileListResponse {
  files: UserFile[]
}

export interface CreateFileRequest {
  name?: string
  path: string
  content?: string
  permissions?: string
}

export interface UpdateFileRequest {
  name?: string
  path?: string
  content?: string
  permissions?: string
}
