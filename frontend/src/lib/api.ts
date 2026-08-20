import { Chat, ChatListResponse } from '../types/chat'
import { loadFullGlobalSettings, getDefaultProxySettings } from '../types/settings'
import { createAgentAPIProxyClientFromStorage } from './agentapi-proxy-client'
import { handleAuthenticationRequired, isAuthenticationRequiredError } from './auth-error-handler'

// Get API configuration from browser storage
function getAPIConfig(): { baseURL: string; apiKey?: string } {
  // Check if we're in a browser environment
  if (typeof window === 'undefined') {
    // Server-side rendering or Node.js environment - use default proxy endpoint
    return {
      baseURL: process.env.NEXT_PUBLIC_API_BASE_URL || 'http://localhost:3000/api/proxy',
      apiKey: process.env.NEXT_PUBLIC_API_KEY || process.env.AGENTAPI_API_KEY,
    };
  }

  try {
    // Get proxy settings from global settings
    const globalSettings = loadFullGlobalSettings();
    const proxySettings = globalSettings.agentApiProxy || getDefaultProxySettings();

    return {
      baseURL: proxySettings.endpoint || process.env.NEXT_PUBLIC_API_BASE_URL || `${window.location.protocol}//${window.location.host}/api/proxy`,
      apiKey: proxySettings.apiKey || process.env.NEXT_PUBLIC_API_KEY || process.env.AGENTAPI_API_KEY,
    };
  } catch (error) {
    console.warn('Failed to load settings from storage for chat API, using fallback:', error);
    return {
      baseURL: process.env.NEXT_PUBLIC_API_BASE_URL || `${window.location.protocol}//${window.location.host}/api/proxy`,
      apiKey: process.env.NEXT_PUBLIC_API_KEY || process.env.AGENTAPI_API_KEY,
    };
  }
}

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message)
    this.name = 'ApiError'
  }
}

async function apiRequest<T>(
  endpoint: string,
  options: RequestInit = {}
): Promise<T> {
  const config = getAPIConfig()
  const url = `${config.baseURL}${endpoint}`
  
  const defaultHeaders: HeadersInit = {
    'Content-Type': 'application/json',
  }

  // Try Bearer token first, then fallback to X-API-Key
  if (config.apiKey) {
    // First attempt with Bearer token
    const bearerHeaders = { ...defaultHeaders, 'Authorization': `Bearer ${config.apiKey}` }
    
    try {
      const response = await fetch(url, {
        ...options,
        headers: {
          ...bearerHeaders,
          ...options.headers,
        },
      })

      if (!response.ok) {
        // If Bearer token fails with 401/403, try X-API-Key fallback
        if (response.status === 401 || response.status === 403) {
          const fallbackHeaders = { ...defaultHeaders, 'X-API-Key': config.apiKey }
          
          const fallbackResponse = await fetch(url, {
            ...options,
            headers: {
              ...fallbackHeaders,
              ...options.headers,
            },
          })

          if (!fallbackResponse.ok) {
            const fallbackErrorData = await fallbackResponse.json().catch(() => ({}))
            if (isAuthenticationRequiredError(fallbackResponse.status, fallbackErrorData)) {
              handleAuthenticationRequired()
            }
            throw new ApiError(
              fallbackResponse.status,
              `API request failed: ${fallbackResponse.status} ${fallbackResponse.statusText}`
            )
          }

          const fallbackData = await fallbackResponse.json()
          return fallbackData
        }
        
        throw new ApiError(
          response.status,
          `API request failed: ${response.status} ${response.statusText}`
        )
      }

      const data = await response.json()
      return data
    } catch (error) {
      if (error instanceof ApiError) {
        throw error
      }
      throw new ApiError(0, `Network error: ${error instanceof Error ? error.message : 'Unknown error'}`)
    }
  }

  // No API key case
  try {
    const response = await fetch(url, {
      ...options,
      headers: {
        ...defaultHeaders,
        ...options.headers,
      },
    })

    if (!response.ok) {
      const errorData = await response.json().catch(() => ({}))
      if (isAuthenticationRequiredError(response.status, errorData)) {
        handleAuthenticationRequired()
      }
      throw new ApiError(
        response.status,
        `API request failed: ${response.status} ${response.statusText}`
      )
    }

    const data = await response.json()
    return data
  } catch (error) {
    if (error instanceof ApiError) {
      throw error
    }
    throw new ApiError(0, `Network error: ${error instanceof Error ? error.message : 'Unknown error'}`)
  }
}

export const chatApi = {
  async getChats(params?: {
    page?: number
    limit?: number
    status?: Chat['status']
    repository?: string
  }): Promise<ChatListResponse> {
    const searchParams = new URLSearchParams()
    
    if (params?.page) searchParams.set('page', params.page.toString())
    if (params?.limit) searchParams.set('limit', params.limit.toString())
    if (params?.status) searchParams.set('status', params.status)
    if (params?.repository) searchParams.set('repository', params.repository)

    const endpoint = `/chats${searchParams.toString() ? `?${searchParams.toString()}` : ''}`
    return apiRequest<ChatListResponse>(endpoint)
  },

  async getChat(id: string): Promise<Chat> {
    return apiRequest<Chat>(`/chats/${id}`)
  },

  async createChat(data: Partial<Chat>): Promise<Chat> {
    return apiRequest<Chat>('/chats', {
      method: 'POST',
      body: JSON.stringify(data),
    })
  },

  async updateChat(id: string, data: Partial<Chat>): Promise<Chat> {
    return apiRequest<Chat>(`/chats/${id}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    })
  },

  async deleteChat(id: string): Promise<void> {
    return apiRequest<void>(`/chats/${id}`, {
      method: 'DELETE',
    })
  },
}

// AgentAPI Proxy client factory
export function createAgentAPIClient(repoFullname?: string) {
  return createAgentAPIProxyClientFromStorage(repoFullname)
}

// Default AgentAPI Proxy client for backward compatibility
export const agentAPI = createAgentAPIProxyClientFromStorage()