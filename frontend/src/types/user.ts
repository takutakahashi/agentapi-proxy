// agentapi-proxy /user/info から取得するユーザー情報
// teams は "org/team-slug" 形式の文字列配列
// repositories は "owner/repo" 形式の文字列配列
export interface ProxyUserInfo {
  username: string
  teams: string[]
  repositories?: string[]
  is_admin?: boolean
}

// 統合されたユーザー情報
export interface UserInfo {
  type: 'github' | 'api_key' | 'proxy'
  user?: {
    // GitHub OAuth 用
    id?: number
    login?: string
    name?: string
    email?: string
    avatar_url?: string
    // 共通
    authenticated?: boolean
  }
  // agentapi-proxy から取得した情報
  proxy?: ProxyUserInfo
}
