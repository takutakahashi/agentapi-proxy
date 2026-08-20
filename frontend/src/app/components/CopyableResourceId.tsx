'use client'

import { useEffect, useRef, useState } from 'react'
import { Check, Copy } from 'lucide-react'

interface CopyableResourceIdProps {
  id: string
  className?: string
}

export default function CopyableResourceId({ id, className = '' }: CopyableResourceIdProps) {
  const [copied, setCopied] = useState(false)
  const resetTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => () => {
    if (resetTimer.current) clearTimeout(resetTimer.current)
  }, [])

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(id)
      setCopied(true)
      if (resetTimer.current) clearTimeout(resetTimer.current)
      resetTimer.current = setTimeout(() => setCopied(false), 2000)
    } catch (error) {
      console.error('Failed to copy resource ID:', error)
    }
  }

  return (
    <button
      type="button"
      onClick={handleCopy}
      className={`group inline-flex max-w-full items-center gap-1 rounded px-1.5 py-0.5 text-xs text-gray-400 transition-colors hover:bg-gray-100 hover:text-blue-600 dark:text-gray-500 dark:hover:bg-gray-700 dark:hover:text-blue-400 ${className}`}
      title={copied ? 'IDをコピーしました' : `${id} をコピー`}
      aria-label={copied ? 'IDをコピーしました' : `ID ${id} をコピー`}
    >
      <span className="truncate font-mono">ID: {id}</span>
      {copied ? (
        <Check className="h-3.5 w-3.5 flex-shrink-0 text-green-500" aria-hidden="true" />
      ) : (
        <Copy className="h-3.5 w-3.5 flex-shrink-0 opacity-60 group-hover:opacity-100" aria-hidden="true" />
      )}
    </button>
  )
}
