'use client'

import { ScheduleStatus } from '../../types/schedule'

interface ScheduleStatusBadgeProps {
  status: ScheduleStatus
  className?: string
}

const statusConfig: Record<ScheduleStatus, { label: string; className: string; icon: React.ReactNode }> = {
  active: {
    label: 'Active',
    className: 'bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-400',
    icon: (
      <svg className="w-3 h-3" fill="currentColor" viewBox="0 0 8 8">
        <circle cx="4" cy="4" r="3" />
      </svg>
    ),
  },
  paused: {
    label: 'Paused',
    className: 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900/30 dark:text-yellow-400',
    icon: (
      <svg className="w-3 h-3" fill="currentColor" viewBox="0 0 24 24">
        <path d="M6 4h4v16H6zm8 0h4v16h-4z" />
      </svg>
    ),
  },
  completed: {
    label: 'Completed',
    className: 'bg-gray-100 text-gray-800 dark:bg-gray-700 dark:text-gray-300',
    icon: (
      <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
      </svg>
    ),
  },
}

export default function ScheduleStatusBadge({ status, className = '' }: ScheduleStatusBadgeProps) {
  const config = statusConfig[status]

  return (
    <span
      className={`
        inline-flex items-center gap-1.5 px-2.5 py-1 text-xs font-medium rounded-full
        ${config.className}
        ${className}
      `}
    >
      {config.icon}
      {config.label}
    </span>
  )
}
