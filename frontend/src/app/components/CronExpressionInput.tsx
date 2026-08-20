'use client'

import { useState } from 'react'
import { CRON_PRESETS, CronPreset } from '../../types/schedule'

interface CronExpressionInputProps {
  value: string
  onChange: (value: string) => void
  error?: string
  disabled?: boolean
}

export default function CronExpressionInput({
  value,
  onChange,
  error,
  disabled = false,
}: CronExpressionInputProps) {
  const [showPresets, setShowPresets] = useState(false)

  const handlePresetClick = (preset: CronPreset) => {
    onChange(preset.value)
    setShowPresets(false)
  }

  const isValidCron = (expr: string): boolean => {
    if (!expr) return true
    const parts = expr.trim().split(/\s+/)
    return parts.length === 5
  }

  const hasError = error || (value && !isValidCron(value))

  return (
    <div className="space-y-2">
      <div className="relative">
        <div className="flex gap-2">
          <input
            type="text"
            value={value}
            onChange={(e) => onChange(e.target.value)}
            placeholder="0 9 * * *"
            disabled={disabled}
            className={`
              flex-1 px-3 py-2 border rounded-md font-mono text-sm
              focus:outline-none focus:ring-2 focus:ring-blue-500
              dark:bg-gray-700 dark:text-white
              ${hasError
                ? 'border-red-300 dark:border-red-600'
                : 'border-gray-300 dark:border-gray-600'
              }
              ${disabled ? 'bg-gray-100 dark:bg-gray-800 cursor-not-allowed' : ''}
            `}
          />
          <button
            type="button"
            onClick={() => setShowPresets(!showPresets)}
            disabled={disabled}
            className="px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md text-gray-700 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-gray-700 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
          >
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
          </button>
        </div>

        {/* Presets Dropdown */}
        {showPresets && (
          <div className="absolute z-10 mt-1 w-full bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md shadow-lg">
            <div className="p-2 border-b border-gray-100 dark:border-gray-700">
              <span className="text-xs font-medium text-gray-500 dark:text-gray-400">
                プリセット
              </span>
            </div>
            <div className="max-h-60 overflow-y-auto">
              {CRON_PRESETS.map((preset) => (
                <button
                  key={preset.value}
                  type="button"
                  onClick={() => handlePresetClick(preset)}
                  className="w-full text-left px-3 py-2 hover:bg-gray-50 dark:hover:bg-gray-700 transition-colors"
                >
                  <div className="flex items-center justify-between">
                    <span className="text-sm font-medium text-gray-900 dark:text-white">
                      {preset.label}
                    </span>
                    <span className="text-xs font-mono text-gray-500 dark:text-gray-400">
                      {preset.value}
                    </span>
                  </div>
                  {preset.description && (
                    <p className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
                      {preset.description}
                    </p>
                  )}
                </button>
              ))}
            </div>
          </div>
        )}
      </div>

      {/* Help Text */}
      <div className="text-xs text-gray-500 dark:text-gray-400">
        <span className="font-mono">分 時 日 月 曜日</span> の形式で入力（例: <span className="font-mono">0 9 * * *</span> = 毎日9時）
      </div>

      {/* Error Message */}
      {hasError && (
        <p className="text-xs text-red-600 dark:text-red-400">
          {error || 'cron式の形式が正しくありません（5つのフィールドが必要です）'}
        </p>
      )}
    </div>
  )
}
