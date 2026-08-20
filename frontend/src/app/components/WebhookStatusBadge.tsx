'use client'

import { WebhookStatus } from '../../types/webhook'

interface WebhookStatusBadgeProps {
  status: WebhookStatus
  className?: string
}

const statusConfig: Record<WebhookStatus, { label: string; className: string; icon: React.ReactNode }> = {
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
}

export default function WebhookStatusBadge({ status, className = '' }: WebhookStatusBadgeProps) {
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
