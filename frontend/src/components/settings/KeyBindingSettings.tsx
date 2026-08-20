'use client'

import { EnterKeyBehavior } from '@/types/settings'

interface KeyBindingSettingsProps {
  enterKeyBehavior: EnterKeyBehavior
  onChange: (behavior: EnterKeyBehavior) => void
}

export function KeyBindingSettings({ enterKeyBehavior, onChange }: KeyBindingSettingsProps) {
  const options: { value: EnterKeyBehavior; label: string; description: string }[] = [
    {
      value: 'send',
      label: 'Enter で送信',
      description: 'Enter キーでメッセージを送信、Command+Enter (または Ctrl+Enter) で改行'
    },
    {
      value: 'newline',
      label: 'Enter で改行',
      description: 'Enter キーで改行、Command+Enter (または Ctrl+Enter) でメッセージを送信'
    }
  ]

  return (
    <div className="space-y-4">
      <div>
        <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
          Enter キーの動作
        </label>
        <p className="text-xs text-gray-500 dark:text-gray-400 mb-3">
          セッション画面でのメッセージ入力時の Enter キーの動作を選択します
        </p>
      </div>
      <div className="space-y-2">
        {options.map((option) => (
          <label
            key={option.value}
            className={`flex items-start p-3 rounded-lg border cursor-pointer transition-colors ${
              enterKeyBehavior === option.value
                ? 'border-blue-500 bg-blue-50 dark:bg-blue-900/20'
                : 'border-gray-200 dark:border-gray-700 hover:bg-gray-50 dark:hover:bg-gray-800'
            }`}
          >
            <input
              type="radio"
              name="enterKeyBehavior"
              value={option.value}
              checked={enterKeyBehavior === option.value}
              onChange={() => onChange(option.value)}
              className="mt-0.5 h-4 w-4 text-blue-600 focus:ring-blue-500"
            />
            <div className="ml-3">
              <span className="block text-sm font-medium text-gray-900 dark:text-white">
                {option.label}
              </span>
              <span className="block text-xs text-gray-500 dark:text-gray-400">
                {option.description}
              </span>
            </div>
          </label>
        ))}
      </div>
    </div>
  )
}
