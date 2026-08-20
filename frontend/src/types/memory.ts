export type MemoryScope = 'user' | 'team'

export interface Memory {
  id: string
  title: string
  content: string
  scope: MemoryScope
  owner_id: string
  team_id?: string
  tags: Record<string, string>
  created_at: string
  updated_at: string
}

export interface CreateMemoryRequest {
  title: string
  scope: MemoryScope
  content?: string
  team_id?: string
  tags?: Record<string, string>
}

export interface UpdateMemoryRequest {
  title?: string
  content?: string
  tags?: Record<string, string>
}

export interface MemoryListResponse {
  memories: Memory[]
  total: number
}

export interface MemoryListParams {
  scope?: MemoryScope
  team_id?: string
  /** include_tag.{key}={value} に展開されるタグフィルター（AND 結合） */
  includeTags?: Record<string, string>
}
