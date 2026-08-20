import { SlackBotStatus } from '../../types/slackbot'

interface SlackbotStatusBadgeProps {
  status: SlackBotStatus
}

const statusConfig: Record<SlackBotStatus, { label: string; className: string; icon: React.ReactNode }> = {
  active: {
    label: 'Active',
    className: 'bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-300',
    icon: (
      <span className="w-1.5 h-1.5 rounded-full bg-green-500 dark:bg-green-400 inline-block mr-1.5" />
    ),
  },
  paused: {
    label: 'Paused',
    className: 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900/30 dark:text-yellow-300',
    icon: (
      <svg className="w-3 h-3 mr-1" fill="currentColor" viewBox="0 0 24 24">
        <path d="M6 19h4V5H6v14zm8-14v14h4V5h-4z" />
      </svg>
    ),
  },
}

export default function SlackbotStatusBadge({ status }: SlackbotStatusBadgeProps) {
  const config = statusConfig[status] || statusConfig.paused

  return (
    <span
      className={`inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium ${config.className}`}
    >
      {config.icon}
      {config.label}
    </span>
  )
}
