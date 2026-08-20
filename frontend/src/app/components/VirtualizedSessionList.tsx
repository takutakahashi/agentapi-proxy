'use client'

import { memo } from 'react'
import { Session } from '../../types/agentapi'
import SessionCard from './SessionCard'

interface SessionListProps {
  sessions: Session[]
  onDelete: (sessionId: string) => void | Promise<void>
  deletingSession: string | null
  selectedSessions: Set<string>
  onToggleSelect: (sessionId: string) => void
  isSelectionMode: boolean
}

/**
 * Memoized SessionCard component for better performance
 */
const MemoizedSessionCard = memo(function MemoizedSessionCard({
  session,
  onDelete,
  isDeleting,
  isSelected,
  onToggleSelect,
  isSelectionMode,
}: {
  session: Session
  onDelete: (sessionId: string) => void | Promise<void>
  isDeleting: boolean
  isSelected: boolean
  onToggleSelect: (sessionId: string) => void
  isSelectionMode: boolean
}) {
  const handleDelete = async () => {
    await onDelete(session.session_id)
  }
  
  return (
    <SessionCard
      session={session}
      onDelete={handleDelete}
      isDeleting={isDeleting}
      isSelected={isSelected}
      onToggleSelect={onToggleSelect}
      isSelectionMode={isSelectionMode}
    />
  )
})

/**
 * Simple session list component with memoized items
 * 
 * This component renders sessions with React.memo for each item,
 * which prevents unnecessary re-renders when the list changes.
 * Combined with SWR caching, this provides good performance
 * for most use cases without the complexity of virtualization.
 * 
 * @param sessions - Array of sessions to display
 * @param onDelete - Callback when a session is deleted
 * @param deletingSession - ID of session currently being deleted
 * @param selectedSessions - Set of selected session IDs
 * @param onToggleSelect - Callback to toggle session selection
 * @param isSelectionMode - Whether selection mode is active
 */
export default function OptimizedSessionList({
  sessions,
  onDelete,
  deletingSession,
  selectedSessions,
  onToggleSelect,
  isSelectionMode,
}: SessionListProps) {
  return (
    <div>
      {sessions.map((session) => (
        <MemoizedSessionCard
          key={session.session_id}
          session={session}
          onDelete={onDelete}
          isDeleting={deletingSession === session.session_id}
          isSelected={selectedSessions.has(session.session_id)}
          onToggleSelect={onToggleSelect}
          isSelectionMode={isSelectionMode}
        />
      ))}
    </div>
  )
}

/**
 * Non-memoized fallback for small lists where memoization overhead isn't worth it
 */
export function SimpleSessionList({
  sessions,
  onDelete,
  deletingSession,
  selectedSessions,
  onToggleSelect,
  isSelectionMode,
}: SessionListProps) {
  return (
    <div>
      {sessions.map((session) => (
        <SessionCard
          key={session.session_id}
          session={session}
          onDelete={async () => { await onDelete(session.session_id) }}
          isDeleting={deletingSession === session.session_id}
          isSelected={selectedSessions.has(session.session_id)}
          onToggleSelect={onToggleSelect}
          isSelectionMode={isSelectionMode}
        />
      ))}
    </div>
  )
}

/**
 * Smart session list that automatically chooses between memoized and simple rendering
 * based on the number of sessions
 */
export function SmartSessionList({
  sessions,
  onDelete,
  deletingSession,
  selectedSessions,
  onToggleSelect,
  isSelectionMode,
  virtualizationThreshold = 50,
}: SessionListProps & { virtualizationThreshold?: number }) {
  // Use memoization only when there are many sessions
  if (sessions.length >= virtualizationThreshold) {
    return (
      <OptimizedSessionList
        sessions={sessions}
        onDelete={onDelete}
        deletingSession={deletingSession}
        selectedSessions={selectedSessions}
        onToggleSelect={onToggleSelect}
        isSelectionMode={isSelectionMode}
      />
    )
  }

  // Use simple rendering for small lists
  return (
    <SimpleSessionList
      sessions={sessions}
      onDelete={onDelete}
      deletingSession={deletingSession}
      selectedSessions={selectedSessions}
      onToggleSelect={onToggleSelect}
      isSelectionMode={isSelectionMode}
    />
  )
}