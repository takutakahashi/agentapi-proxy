'use client'

import useSWR, { SWRConfiguration, mutate } from 'swr'
import { Session, SessionListParams } from '../types/agentapi'
import { AgentAPIProxyClient, createAgentAPIProxyClientFromStorage, AgentAPIProxyError } from '../lib/agentapi-proxy-client'

// Global mutation key for session list
export const SESSION_LIST_MUTATION_KEY = 'session-list'

/**
 * Fetcher function for session list
 */
async function fetchSessionList(params: SessionListParams, client?: AgentAPIProxyClient): Promise<Session[]> {
  const agentAPI = client || createAgentAPIProxyClientFromStorage()
  const response = await agentAPI.search(params)
  return response.sessions || []
}

/**
 * Custom hook for fetching session list with SWR caching
 * 
 * Features:
 * - Automatic caching with SWR
 * - Deduplication of requests
 * - Background refetching
 * - Error handling
 * 
 * @param params - Query parameters for session list
 * @param config - SWR configuration options
 */
export function useSessionList(
  params: SessionListParams,
  client?: AgentAPIProxyClient,
  config?: SWRConfiguration
) {
  // Create a stable key for SWR based on params
  const key = [
    'session-list',
    params.user_id || '',
    params.scope || '',
    params.team_id || '',
    params.status || '',
    params.page || '',
    params.limit || '',
  ]
  
  const { data, error, isLoading, mutate } = useSWR<Session[], AgentAPIProxyError>(
    key,
    () => fetchSessionList(params, client),
    {
      // Refresh interval for near real-time updates (30 seconds)
      refreshInterval: 30000,
      // Revalidate on window focus
      revalidateOnFocus: true,
      // Dedup requests within this time window
      dedupingInterval: 5000,
      // Don't retry on error immediately
      shouldRetryOnError: false,
      ...config,
    }
  )

  return {
    sessions: data || [],
    isLoading,
    isError: !!error,
    error,
    mutate,
  }
}

/**
 * Custom hook for fetching a single session status
 * Uses SSE for real-time updates when available, falls back to polling
 * 
 * @param sessionId - Session ID to watch
 * @param config - SWR configuration options
 */
export function useSessionStatus(
  sessionId: string | null,
  config?: SWRConfiguration
) {
  const { data, error, isLoading, mutate } = useSWR(
    sessionId ? ['session-status', sessionId] : null,
    async () => {
      if (!sessionId) return null
      const agentAPI = createAgentAPIProxyClientFromStorage()
      return agentAPI.getSessionStatus(sessionId)
    },
    {
      refreshInterval: 5000, // Poll every 5 seconds as fallback
      revalidateOnFocus: true,
      ...config,
    }
  )

  return {
    status: data,
    isLoading,
    isError: !!error,
    error,
    mutate,
  }
}

/**
 * Utility function to mutate all session list caches
 * Call this after creating/deleting sessions
 */
export async function mutateSessionList() {
  // Mutate all session-list keys
  await mutate(
    (key) => Array.isArray(key) && key[0] === 'session-list',
    undefined,
    { revalidate: true }
  )
}

/**
 * Utility function to optimistically update session status in cache
 * Useful for immediate UI updates before server confirms
 */
export function optimisticUpdateSessionStatus(
  sessionId: string,
  newStatus: string
) {
  mutate(
    (key) => Array.isArray(key) && key[0] === 'session-list',
    (currentData: Session[] | undefined) => {
      if (!currentData) return currentData
      return currentData.map(session =>
        session.session_id === sessionId
          ? { ...session, status: newStatus as Session['status'] }
          : session
      )
    },
    false // Don't revalidate immediately
  )
}
