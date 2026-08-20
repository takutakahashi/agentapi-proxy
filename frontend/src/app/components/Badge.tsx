interface BadgeProps {
  children: React.ReactNode
  variant: 'default' | 'secondary' | 'destructive' | 'outline'
}

export default function Badge({ children, variant }: BadgeProps) {
  const variants: Record<string, string> = {
    default: 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200',
    secondary: 'bg-gray-100 text-gray-800 dark:bg-gray-700 dark:text-gray-200',
    destructive: 'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200',
    outline: 'bg-white text-gray-600 border border-gray-300 dark:bg-gray-800 dark:text-gray-300 dark:border-gray-600',
  }
  
  return (
    <span className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${variants[variant] || variants.outline}`}>
      {children}
    </span>
  )
}