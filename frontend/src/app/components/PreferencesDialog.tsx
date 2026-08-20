'use client'

import { useCallback, useEffect, useRef, useState } from 'react'
import {
  EnterKeyBehavior,
  FontSettings as FontSettingsType,
  getEnterKeyBehavior,
  getFontSettings,
  setEnterKeyBehavior,
  setFontSettings,
} from '@/types/settings'
import { FontSettings, KeyBindingSettings } from '@/components/settings'

interface PreferencesDialogProps {
  isOpen: boolean
  onClose: () => void
}

const FOCUSABLE_SELECTOR =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'

export default function PreferencesDialog({ isOpen, onClose }: PreferencesDialogProps) {
  const [enterKeyBehavior, setEnterKeyBehaviorState] = useState<EnterKeyBehavior>('newline')
  const [fontSettings, setFontSettingsState] = useState<FontSettingsType>({
    fontSize: 14,
    fontFamily: 'sans-serif',
  })
  const [savedAt, setSavedAt] = useState<number | null>(null)
  const dialogRef = useRef<HTMLDivElement>(null)
  const savedTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  // 開くたびに localStorage から最新値を読み込む
  useEffect(() => {
    if (!isOpen) return
    setEnterKeyBehaviorState(getEnterKeyBehavior())
    setFontSettingsState(getFontSettings())
  }, [isOpen])

  const notifySaved = useCallback(() => {
    setSavedAt(Date.now())
    if (savedTimerRef.current) clearTimeout(savedTimerRef.current)
    savedTimerRef.current = setTimeout(() => setSavedAt(null), 2000)
  }, [])

  useEffect(() => {
    return () => {
      if (savedTimerRef.current) clearTimeout(savedTimerRef.current)
    }
  }, [])

  // Esc で閉じる / Tab をダイアログ内に閉じ込める
  useEffect(() => {
    if (!isOpen) return

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        onClose()
        return
      }
      if (e.key !== 'Tab' || !dialogRef.current) return

      const focusable = Array.from(
        dialogRef.current.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR)
      )
      if (focusable.length === 0) return

      const first = focusable[0]
      const last = focusable[focusable.length - 1]
      const active = document.activeElement

      if (e.shiftKey && (active === first || !dialogRef.current.contains(active))) {
        e.preventDefault()
        last.focus()
      } else if (!e.shiftKey && active === last) {
        e.preventDefault()
        first.focus()
      }
    }

    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [isOpen, onClose])

  // 開いたら最初の要素にフォーカスする
  useEffect(() => {
    if (!isOpen || !dialogRef.current) return
    const focusable = dialogRef.current.querySelector<HTMLElement>(FOCUSABLE_SELECTOR)
    focusable?.focus()
  }, [isOpen])

  const handleEnterKeyBehaviorChange = (behavior: EnterKeyBehavior) => {
    setEnterKeyBehaviorState(behavior)
    setEnterKeyBehavior(behavior)
    notifySaved()
  }

  const handleFontSettingsChange = (settings: FontSettingsType) => {
    setFontSettingsState(settings)
    setFontSettings(settings)
    notifySaved()
  }

  if (!isOpen) return null

  return (
    <div
      className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4"
      onClick={onClose}
    >
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby="preferences-dialog-title"
        className="bg-white dark:bg-gray-800 rounded-lg w-full max-w-lg shadow-xl overflow-hidden max-h-[85vh] flex flex-col"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between px-6 py-4 border-b border-gray-200 dark:border-gray-700">
          <div className="flex items-center gap-3">
            <h2
              id="preferences-dialog-title"
              className="text-lg font-semibold text-gray-900 dark:text-white"
            >
              環境設定
            </h2>
            <span className="px-2 py-0.5 text-xs font-medium rounded-full bg-emerald-50 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300">
              このブラウザのみ
            </span>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
            aria-label="閉じる"
          >
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>

        <div className="px-6 py-4 overflow-y-auto space-y-6">
          <p className="text-xs text-gray-500 dark:text-gray-400">
            この設定はこのブラウザにのみ保存され、他の端末には同期されません。変更はすぐに反映されます。
          </p>

          <FontSettings fontSettings={fontSettings} onChange={handleFontSettingsChange} />

          <div className="border-t border-gray-200 dark:border-gray-700 pt-6">
            <KeyBindingSettings
              enterKeyBehavior={enterKeyBehavior}
              onChange={handleEnterKeyBehaviorChange}
            />
          </div>
        </div>

        <div className="flex items-center justify-between px-6 py-3 border-t border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-800/50">
          <span
            aria-live="polite"
            className={`flex items-center gap-1.5 text-xs text-green-600 dark:text-green-400 transition-opacity ${
              savedAt ? 'opacity-100' : 'opacity-0'
            }`}
          >
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
            </svg>
            保存しました
          </span>
          <button
            type="button"
            onClick={onClose}
            className="px-4 py-1.5 text-sm font-medium text-gray-700 dark:text-gray-300 border border-gray-300 dark:border-gray-600 rounded-md hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors"
          >
            閉じる
          </button>
        </div>
      </div>
    </div>
  )
}
