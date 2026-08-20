'use client'

import { useState, useEffect, useCallback } from 'react'
import { Chat, ChatListResponse } from '../../../types/chat'
import { chatApi, ApiError } from '../../../lib/api'
import ConversationCard from '../../components/ConversationCard'
import LoadingSpinner from '../../components/LoadingSpinner'
import Link from 'next/link'

interface FilterState {
  status: Chat['status'] | 'all'
  page: number
  limit: number
}

interface RepositoryConversationListProps {
  repository: string
}

export default function RepositoryConversationList({ repository }: RepositoryConversationListProps) {
  const [chats, setChats] = useState<Chat[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [totalChats, setTotalChats] = useState(0)
  const [filters, setFilters] = useState<FilterState>({
    status: 'all',
    page: 1,
    limit: 20
  })

  const statusOptions: Array<{ value: Chat['status'] | 'all'; label: string; count?: number }> = [
    { value: 'all', label: 'All' },
    { value: 'running', label: 'Running' },
    { value: 'completed', label: 'Completed' },
    { value: 'failed', label: 'Failed' },
    { value: 'pending', label: 'Pending' },
    { value: 'cancelled', label: 'Cancelled' },
  ]

  const fetchChats = useCallback(async () => {
    try {
      setLoading(true)
      setError(null)

      const params = {
        page: filters.page,
        limit: filters.limit,
        repository: repository,
        ...(filters.status !== 'all' && { status: filters.status }),
      }

      const response: ChatListResponse = await chatApi.getChats(params)
      setChats(response.chats)
      setTotalChats(response.total)
    } catch (err) {
      if (err instanceof ApiError) {
        setError(`Failed to load conversations: ${err.message}`)
      } else {
        setError('An unexpected error occurred while loading conversations')
      }
      
      // Set mock data for demo purposes when API is not available
      // Filter mock data by repository
      const mockChats = getMockChats().filter(chat => 
        chat.metadata?.repository === repository ||
        chat.metadata?.repository_name === repository ||
        (chat.metadata?.repository_owner && chat.metadata?.repository_name && 
         `${chat.metadata.repository_owner}/${chat.metadata.repository_name}` === repository)
      )
      setChats(mockChats)
      setTotalChats(mockChats.length)
    } finally {
      setLoading(false)
    }
  }, [filters, repository])

  useEffect(() => {
    fetchChats()
  }, [fetchChats])

  const getMockChats = (): Chat[] => [
    {
      id: 'chat-001',
      title: 'Code Review Discussion',
      summary: 'Discussing best practices for React component architecture and performance optimization strategies.',
      status: 'completed',
      created_at: new Date(Date.now() - 2 * 60 * 60 * 1000).toISOString(),
      updated_at: new Date(Date.now() - 30 * 60 * 1000).toISOString(),
      message_count: 15,
      duration: 1200,
      metadata: {
        repository: 'orgs/example-repo',
        repository_owner: 'orgs',
        repository_name: 'example-repo',
        branch: 'main',
        commit_hash: 'abc123'
      }
    },
    {
      id: 'chat-002',
      title: 'API Integration Help',
      summary: 'Working through REST API integration issues and authentication flow problems.',
      status: 'running',
      created_at: new Date(Date.now() - 45 * 60 * 1000).toISOString(),
      updated_at: new Date(Date.now() - 5 * 60 * 1000).toISOString(),
      message_count: 8,
      duration: 2400,
      metadata: {
        repository: 'users/demo-project',
        repository_owner: 'users',
        repository_name: 'demo-project',
        branch: 'feature/api-integration'
      }
    },
    {
      id: 'chat-003',
      title: 'Database Schema Design',
      summary: 'Designing optimal database schema for a multi-tenant application with complex relationships.',
      status: 'failed',
      created_at: new Date(Date.now() - 24 * 60 * 60 * 1000).toISOString(),
      updated_at: new Date(Date.now() - 22 * 60 * 60 * 1000).toISOString(),
      message_count: 23,
      duration: 3600,
      metadata: {
        repository: 'orgs/example-repo',
        repository_owner: 'orgs',
        repository_name: 'example-repo',
        branch: 'develop'
      }
    },
    {
      id: 'chat-004',
      title: 'Frontend Testing Strategy',
      summary: 'Exploring different testing approaches for React applications including unit, integration, and e2e tests.',
      status: 'pending',
      created_at: new Date(Date.now() - 3 * 24 * 60 * 60 * 1000).toISOString(),
      updated_at: new Date(Date.now() - 2 * 24 * 60 * 60 * 1000).toISOString(),
      message_count: 5,
      duration: 600,
      metadata: {
        repository: 'users/test-app',
        repository_owner: 'users',
        repository_name: 'test-app'
      }
    }
  ]

  const handleStatusFilter = (status: Chat['status'] | 'all') => {
    setFilters(prev => ({ ...prev, status, page: 1 }))
  }

  const handlePageChange = (newPage: number) => {
    setFilters(prev => ({ ...prev, page: newPage }))
  }

  const filteredChats = filters.status === 'all' 
    ? chats 
    : chats.filter(chat => chat.status === filters.status)

  const totalPages = Math.ceil(totalChats / filters.limit)

  if (loading && chats.length === 0) {
    return <LoadingSpinner />
  }

  return (
    <div className="space-y-6">
      {/* Navigation */}
      <div className="flex items-center gap-2 text-sm">
        <Link 
          href="/chats" 
          className="text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300"
        >
          All Conversations
        </Link>
        <span className="text-gray-400 dark:text-gray-500">→</span>
        <span className="text-gray-600 dark:text-gray-400">
          {repository}
        </span>
      </div>

      {/* Filter Bar */}
      <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg p-4">
        <div className="flex flex-wrap items-center gap-3">
          <span className="text-sm font-medium text-gray-700 dark:text-gray-300">
            Filter by status:
          </span>
          <div className="flex flex-wrap gap-2">
            {statusOptions.map((option) => (
              <button
                key={option.value}
                onClick={() => handleStatusFilter(option.value)}
                className={`px-3 py-1.5 text-sm rounded-md border transition-colors ${
                  filters.status === option.value
                    ? 'bg-blue-100 border-blue-300 text-blue-800 dark:bg-blue-900 dark:border-blue-600 dark:text-blue-200'
                    : 'bg-white border-gray-300 text-gray-700 hover:bg-gray-50 dark:bg-gray-700 dark:border-gray-600 dark:text-gray-300 dark:hover:bg-gray-600'
                }`}
              >
                {option.label}
              </button>
            ))}
          </div>
        </div>

        <div className="mt-3 flex items-center justify-between">
          <span className="text-sm text-gray-500 dark:text-gray-400">
            {totalChats} conversation{totalChats !== 1 ? 's' : ''} for {repository}
          </span>
          <button
            onClick={fetchChats}
            disabled={loading}
            className="text-sm text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300 disabled:opacity-50"
          >
            {loading ? 'Refreshing...' : 'Refresh'}
          </button>
        </div>
      </div>

      {/* Error State */}
      {error && (
        <div className="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg p-4">
          <div className="flex items-center">
            <span className="text-red-600 dark:text-red-400 text-sm">{error}</span>
          </div>
        </div>
      )}

      {/* Conversation List */}
      <div className="space-y-4">
        {filteredChats.length === 0 ? (
          <div className="text-center py-12">
            <div className="text-gray-400 dark:text-gray-500 mb-2">
              <svg className="w-12 h-12 mx-auto" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z" />
              </svg>
            </div>
            <h3 className="text-lg font-medium text-gray-900 dark:text-white mb-1">
              No conversations found
            </h3>
            <p className="text-gray-500 dark:text-gray-400">
              {filters.status === 'all' 
                ? `No conversations found for repository "${repository}".`
                : `No conversations with status "${filters.status}" for repository "${repository}".`
              }
            </p>
          </div>
        ) : (
          <>
            {filteredChats.map((chat) => (
              <ConversationCard key={chat.id} chat={chat} />
            ))}

            {/* Pagination */}
            {totalPages > 1 && (
              <div className="flex items-center justify-center gap-2 pt-6">
                <button
                  onClick={() => handlePageChange(filters.page - 1)}
                  disabled={filters.page === 1}
                  className="px-3 py-2 text-sm border border-gray-300 dark:border-gray-600 rounded-md disabled:opacity-50 disabled:cursor-not-allowed hover:bg-gray-50 dark:hover:bg-gray-700"
                >
                  Previous
                </button>
                
                <span className="px-4 py-2 text-sm text-gray-700 dark:text-gray-300">
                  Page {filters.page} of {totalPages}
                </span>
                
                <button
                  onClick={() => handlePageChange(filters.page + 1)}
                  disabled={filters.page === totalPages}
                  className="px-3 py-2 text-sm border border-gray-300 dark:border-gray-600 rounded-md disabled:opacity-50 disabled:cursor-not-allowed hover:bg-gray-50 dark:hover:bg-gray-700"
                >
                  Next
                </button>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  )
}