export interface Chat {
  id: string
  title?: string
  summary?: string
  status: 'running' | 'completed' | 'failed' | 'pending' | 'cancelled'
  created_at: string
  updated_at: string
  message_count?: number
  duration?: number
  metadata?: {
    repository?: string
    repository_url?: string
    repository_owner?: string
    repository_name?: string
    branch?: string
    commit_hash?: string
    [key: string]: unknown
  }
}

export interface ChatListResponse {
  chats: Chat[]
  total: number
  page: number
  limit: number
}

export interface Message {
  id: string
  chat_id: string
  role: 'user' | 'assistant' | 'system'
  content: string
  timestamp: string
  metadata?: Record<string, unknown>
}