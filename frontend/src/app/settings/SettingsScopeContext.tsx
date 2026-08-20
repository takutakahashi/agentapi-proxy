'use client'

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import { SettingsData, prepareSettingsForSave } from '@/types/settings'
import {
  AgentAPIProxyError,
  CredentialsMetadata,
  createAgentAPIProxyClientFromStorage,
} from '@/lib/agentapi-proxy-client'
import { useToast } from '@/contexts/ToastContext'
import { SettingsScopeKind } from './navConfig'

/** 直前の状態から差分を組み立てたい場合は関数を渡す */
type SettingsUpdate = Partial<SettingsData> | ((prev: SettingsData) => Partial<SettingsData>)

interface SettingsScopeValue {
  scopeKind: SettingsScopeKind
  /** 設定 API に渡す名前。personal はユーザー名、team はチーム名 */
  scopeId: string
  /** ログイン中のユーザー名。scopeKind に関わらず常に自分自身 */
  userName: string
  userTeams: string[]

  settings: SettingsData
  update: (partial: SettingsUpdate) => void
  save: () => Promise<void>
  discard: () => void
  dirty: boolean
  dirtyFields: (keyof SettingsData)[]

  loading: boolean
  /** 設定の読み込みに成功して編集できる状態か */
  ready: boolean
  saving: boolean
  error: string | null
  isAuthError: boolean

  /** Codex 認証ファイルのメタデータ。専用 API で即時反映される */
  credentialsMetadata: CredentialsMetadata | null
  reloadCredentials: () => Promise<void>
  clearCredentialsMetadata: () => void

  /**
   * 保存レスポンスに一度だけ含まれる External Session Manager の接続トークン。
   * ページを移動しても失われないよう Provider が保持する。
   */
  revealedTokens: Record<string, string>
  regenerateEsmToken: (esmId: string) => Promise<void>
  regeneratingEsmId: string | null
}

const SettingsScopeContext = createContext<SettingsScopeValue | null>(null)

export const useSettingsScope = (): SettingsScopeValue => {
  const context = useContext(SettingsScopeContext)
  if (!context) {
    throw new Error('useSettingsScope must be used within a SettingsScopeProvider')
  }
  return context
}

const collectDirtyFields = (
  current: SettingsData,
  original: SettingsData
): (keyof SettingsData)[] => {
  const keys = new Set<keyof SettingsData>([
    ...(Object.keys(current) as (keyof SettingsData)[]),
    ...(Object.keys(original) as (keyof SettingsData)[]),
  ])
  return Array.from(keys).filter(
    (key) => JSON.stringify(current[key]) !== JSON.stringify(original[key])
  )
}

interface SettingsScopeProviderProps {
  scopeKind: SettingsScopeKind
  /** scopeKind が 'team' のときのチーム名 */
  teamId?: string
  children: React.ReactNode
}

export function SettingsScopeProvider({ scopeKind, teamId, children }: SettingsScopeProviderProps) {
  const [settings, setSettings] = useState<SettingsData>({})
  const [originalSettings, setOriginalSettings] = useState<SettingsData>({})
  const [userName, setUserName] = useState('')
  const [userTeams, setUserTeams] = useState<string[]>([])
  const [loading, setLoading] = useState(true)
  const [ready, setReady] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [isAuthError, setIsAuthError] = useState(false)
  const [credentialsMetadata, setCredentialsMetadata] = useState<CredentialsMetadata | null>(null)
  const [revealedTokens, setRevealedTokens] = useState<Record<string, string>>({})
  const [regeneratingEsmId, setRegeneratingEsmId] = useState<string | null>(null)
  const { showToast } = useToast()

  const scopeId = scopeKind === 'personal' ? userName : (teamId ?? '')

  const dirtyFields = useMemo(
    () => collectDirtyFields(settings, originalSettings),
    [settings, originalSettings]
  )
  const dirty = dirtyFields.length > 0

  // ページ離脱時の警告
  const dirtyRef = useRef(false)
  useEffect(() => {
    dirtyRef.current = dirty
  }, [dirty])

  useEffect(() => {
    const handleBeforeUnload = (e: BeforeUnloadEvent) => {
      if (dirtyRef.current) {
        e.preventDefault()
        e.returnValue = ''
      }
    }
    window.addEventListener('beforeunload', handleBeforeUnload)
    return () => window.removeEventListener('beforeunload', handleBeforeUnload)
  }, [])

  // ユーザー情報の取得
  useEffect(() => {
    let cancelled = false
    const loadUserInfo = async () => {
      try {
        const client = createAgentAPIProxyClientFromStorage()
        const info = await client.getUserInfo()
        if (cancelled) return
        if (info?.username) {
          setUserName(info.username)
          setUserTeams(info.teams || [])
        } else {
          setError('ユーザー情報の取得に失敗しました')
          setLoading(false)
        }
      } catch (err) {
        if (cancelled) return
        console.error('Failed to get user info:', err)
        if (err instanceof AgentAPIProxyError && err.status === 401) {
          setIsAuthError(true)
          setError('認証が必要です。ログアウトして再度ログインしてください。')
        } else {
          setError('ユーザー情報の取得に失敗しました')
        }
        setLoading(false)
      }
    }
    loadUserInfo()
    return () => {
      cancelled = true
    }
  }, [])

  // スコープの設定を読み込む
  useEffect(() => {
    if (!scopeId) return
    let cancelled = false

    const fetchSettings = async () => {
      setLoading(true)
      try {
        const client = createAgentAPIProxyClientFromStorage()
        const data = await client.getSettings(scopeId)
        if (cancelled) return
        setSettings(data)
        setOriginalSettings(data)
        setReady(true)

        try {
          const meta = await client.getCredentials(scopeId)
          if (!cancelled) setCredentialsMetadata(meta)
        } catch {
          // credentials endpoint が無いスコープでは無視する
        }
      } catch (err) {
        if (cancelled) return
        console.error('Failed to load settings:', err)
        if (err instanceof AgentAPIProxyError && err.status === 401) {
          setIsAuthError(true)
          setError('認証が必要です。ログアウトして再度ログインしてください。')
        } else if (scopeKind === 'team') {
          setError('チーム設定を読み込めませんでした。チームが存在しないか、権限がない可能性があります。')
        } else {
          setError('設定の読み込みに失敗しました')
        }
      } finally {
        if (!cancelled) setLoading(false)
      }
    }

    fetchSettings()
    return () => {
      cancelled = true
    }
  }, [scopeId, scopeKind])

  const update = useCallback((partial: SettingsUpdate) => {
    setSettings((prev) => ({
      ...prev,
      ...(typeof partial === 'function' ? partial(prev) : partial),
    }))
  }, [])

  const discard = useCallback(() => {
    setSettings(originalSettings)
  }, [originalSettings])

  const save = useCallback(async () => {
    if (!scopeId) return
    setSaving(true)
    setError(null)
    try {
      const client = createAgentAPIProxyClientFromStorage()
      const saved = await client.saveSettings(scopeId, prepareSettingsForSave(settings))
      setSettings(saved)
      setOriginalSettings(saved)

      // 保存レスポンスに一度だけ含まれる接続トークンを取りこぼさない
      const tokens: Record<string, string> = {}
      for (const manager of saved?.external_session_managers ?? []) {
        if (manager.id && manager.connection_token) {
          tokens[manager.id] = manager.connection_token
        }
      }
      if (Object.keys(tokens).length > 0) {
        setRevealedTokens((prev) => ({ ...prev, ...tokens }))
        showToast('接続トークンを発行しました。必要な値をコピーしてください。', 'success')
      } else {
        showToast('設定を保存しました', 'success')
      }
    } catch (err) {
      console.error('Failed to save settings:', err)
      setError('設定の保存に失敗しました')
      showToast('設定の保存に失敗しました', 'error')
    } finally {
      setSaving(false)
    }
  }, [scopeId, settings, showToast])

  const reloadCredentials = useCallback(async () => {
    if (!scopeId) return
    try {
      const client = createAgentAPIProxyClientFromStorage()
      setCredentialsMetadata(await client.getCredentials(scopeId))
    } catch {
      // 取得できない場合は現在の表示を維持する
    }
  }, [scopeId])

  const clearCredentialsMetadata = useCallback(() => {
    setCredentialsMetadata(null)
  }, [])

  const regenerateEsmToken = useCallback(
    async (esmId: string) => {
      setRegeneratingEsmId(esmId)
      try {
        const client = createAgentAPIProxyClientFromStorage()
        const rotated = await client.rotateExternalSessionManagerToken(esmId)
        setSettings((prev) => ({
          ...prev,
          external_session_managers: (prev.external_session_managers ?? []).map((manager) =>
            manager.id === esmId ? { ...manager, has_connection_token: true } : manager
          ),
        }))
        setOriginalSettings((prev) => ({
          ...prev,
          external_session_managers: (prev.external_session_managers ?? []).map((manager) =>
            manager.id === esmId ? { ...manager, has_connection_token: true } : manager
          ),
        }))
        setRevealedTokens((prev) => ({ ...prev, [esmId]: rotated.connection_token || '' }))
        showToast('接続トークンを再発行しました', 'success')
      } catch (err) {
        console.error('Failed to regenerate connection token:', err)
        showToast('接続トークンの再発行に失敗しました', 'error')
      } finally {
        setRegeneratingEsmId(null)
      }
    },
    [showToast]
  )

  const value: SettingsScopeValue = {
    scopeKind,
    scopeId,
    userName,
    userTeams,
    settings,
    update,
    save,
    discard,
    dirty,
    dirtyFields,
    loading,
    ready,
    saving,
    error,
    isAuthError,
    credentialsMetadata,
    reloadCredentials,
    clearCredentialsMetadata,
    revealedTokens,
    regenerateEsmToken,
    regeneratingEsmId,
  }

  return <SettingsScopeContext.Provider value={value}>{children}</SettingsScopeContext.Provider>
}
