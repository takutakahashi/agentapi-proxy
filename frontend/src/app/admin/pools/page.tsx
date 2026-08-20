'use client'

import { FormEvent, useCallback, useEffect, useMemo, useState } from 'react'
import { createCurrentDeploymentAgentAPIProxyClient } from '@/lib/agentapi-proxy-client'
import {
  ClusterSessionManager,
  LogicalSessionPool,
  SessionPoolBinding,
  SessionPoolSupplier,
} from '@/types/session_pool'
import {
  ItemList,
  ItemListEmpty,
  ItemListRow,
  RowAction,
  SettingsPageHeader,
  SettingsSubsection,
} from '@/components/settings'

export default function SessionPoolsAdminPage() {
  const client = useMemo(() => createCurrentDeploymentAgentAPIProxyClient(), [])
  const [managers, setManagers] = useState<ClusterSessionManager[]>([])
  const [pools, setPools] = useState<LogicalSessionPool[]>([])
  const [suppliers, setSuppliers] = useState<SessionPoolSupplier[]>([])
  const [bindings, setBindings] = useState<Record<string, SessionPoolBinding[]>>({})
  const [managerName, setManagerName] = useState('')
  const [managerID, setManagerID] = useState('')
  const [poolName, setPoolName] = useState('')
  const [supplierPool, setSupplierPool] = useState('')
  const [minIdle, setMinIdle] = useState(1)
  const [maxRunners, setMaxRunners] = useState(10)
  const [subjectType, setSubjectType] = useState<'user' | 'team' | 'all'>('team')
  const [subjectID, setSubjectID] = useState('')
  const [bindingRole, setBindingRole] = useState<'use' | 'manage'>('use')
  const [bindingPool, setBindingPool] = useState('')
  const [bindingPriority, setBindingPriority] = useState(0)
  const [maxConcurrent, setMaxConcurrent] = useState(0)
  const [connectionToken, setConnectionToken] = useState('')
  const [error, setError] = useState('')

  const reload = useCallback(async () => {
    try {
      const [nextManagers, nextPools] = await Promise.all([
        client.listClusterSessionManagers(),
        client.listSessionPools(),
      ])
      const nextSuppliers = (await Promise.all(
        nextManagers.map((manager) => client.listSessionPoolSuppliers(manager.id)),
      )).flat()
      const nextBindings = await Promise.all(
        nextPools.map(async (pool) => [pool.name, await client.listSessionPoolBindings(pool.name)] as const),
      )
      setManagers(nextManagers)
      setPools(nextPools)
      setSuppliers(nextSuppliers)
      setBindings(Object.fromEntries(nextBindings))
      setManagerID((current) => current || nextManagers[0]?.id || '')
      setSupplierPool((current) => current || nextPools[0]?.name || '')
      setBindingPool((current) => current || nextPools[0]?.name || '')
      setError('')
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '読み込みに失敗しました')
    }
  }, [client])

  useEffect(() => { void reload() }, [reload])

  const addManager = async (event: FormEvent) => {
    event.preventDefault()
    try {
      const result = await client.createClusterSessionManager(managerName)
      setConnectionToken(result.connection_token)
      setManagerName('')
      setManagerID(result.manager.id)
      await reload()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Manager登録に失敗しました')
    }
  }

  const addLogicalPool = async (event: FormEvent) => {
    event.preventDefault()
    try {
      const pool = await client.createSessionPool({ name: poolName })
      setPoolName('')
      setSupplierPool(pool.name)
      setBindingPool(pool.name)
      await reload()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Logical Pool作成に失敗しました')
    }
  }

  const addSupplier = async (event: FormEvent) => {
    event.preventDefault()
    try {
      await client.createSessionPoolSupplier(managerID, {
        pool: supplierPool,
        min_idle: minIdle,
        max_runners: maxRunners,
      })
      await reload()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Pool Supplier追加に失敗しました')
    }
  }

  const addBinding = async (event: FormEvent) => {
    event.preventDefault()
    try {
      await client.createSessionPoolBinding(bindingPool, subjectType, subjectID, bindingRole, bindingPriority, maxConcurrent)
      setSubjectID('')
      await reload()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Pool Binding作成に失敗しました')
    }
  }

  const removeBinding = async (pool: string, bindingID: string) => {
    try {
      await client.deleteSessionPoolBinding(pool, bindingID)
      await reload()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Pool Binding削除に失敗しました')
    }
  }

  const removeManager = async (manager: ClusterSessionManager) => {
    if (!window.confirm(`Session Manager「${manager.name}」を削除しますか？関連するSupplierと待機Runnerも削除されます。`)) return
    try {
      await client.deleteClusterSessionManager(manager.id)
      await reload()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Session Manager削除に失敗しました')
    }
  }

  const removePool = async (pool: LogicalSessionPool) => {
    if (!window.confirm(`Logical Pool「${pool.name}」を削除しますか？Supplier、Bindingなどの関連リソースも削除されます。`)) return
    try {
      await client.deleteSessionPool(pool.name)
      await reload()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Logical Pool削除に失敗しました')
    }
  }

  const removeSupplier = async (supplier: SessionPoolSupplier) => {
    const manager = managers.find((item) => item.id === supplier.manager_id)
    if (!window.confirm(`${manager?.name || supplier.manager_id} の「${supplier.pool}」Supplierを削除しますか？待機Runnerも削除されます。`)) return
    try {
      await client.deleteSessionPoolSupplier(supplier.manager_id, supplier.pool)
      await reload()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Pool Supplier削除に失敗しました')
    }
  }

  const input =
    'w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 focus:border-transparent focus:outline-none focus:ring-2 focus:ring-blue-500 dark:border-gray-700 dark:bg-gray-800 dark:text-white'
  const stepCard = 'space-y-3 rounded-lg border border-gray-200 p-4 dark:border-gray-700'
  const stepButton =
    'w-fit rounded-md bg-blue-600 px-3 py-1.5 text-sm font-semibold text-white transition-colors hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-50'
  const stepNumber =
    'flex h-6 w-6 flex-shrink-0 items-center justify-center rounded-md bg-blue-50 text-xs font-bold tabular-nums text-blue-600 dark:bg-blue-900/40 dark:text-blue-300'

  return (
    <>
      <SettingsPageHeader
        title="Session Pools"
        description="Logical Pool、Manager ごとの供給設定、user / team の利用権限をクラスタ全体で管理します。"
      />

      {error && (
        <div role="alert" className="mb-5 rounded-lg border border-red-200 bg-red-50 p-4 dark:border-red-800 dark:bg-red-900/20">
          <p className="text-sm text-red-600 dark:text-red-400">{error}</p>
        </div>
      )}

      {connectionToken && (
        <div className="mb-5 rounded-lg border border-amber-300 bg-amber-50 p-4 dark:border-amber-700 dark:bg-amber-900/20">
          <p className="text-sm font-semibold text-amber-900 dark:text-amber-200">
            Connection token は今だけ表示されます
          </p>
          <code className="mt-2 block break-all font-mono text-xs text-amber-900 dark:text-amber-200">
            {connectionToken}
          </code>
          <button
            type="button"
            className="mt-2 text-xs text-amber-900 underline dark:text-amber-200"
            onClick={() => setConnectionToken('')}
          >
            閉じる
          </button>
        </div>
      )}

      <SettingsSubsection
        title="セットアップ"
        description="Manager から Binding まで、上から順に登録します"
      >
        <div className="grid gap-4 md:grid-cols-2">
          <form onSubmit={addManager} className={stepCard}>
            <div className="flex items-center gap-2">
              <span className={stepNumber}>1</span>
              <h4 className="text-sm font-semibold text-gray-900 dark:text-white">Session Manager</h4>
            </div>
            <p className="text-xs text-gray-500 dark:text-gray-400">Pool を供給する実行環境を登録します。</p>
            <input required className={input} value={managerName} onChange={(event) => setManagerName(event.target.value)} placeholder="Tokyo Kubernetes" />
            <button className={stepButton}>Manager を登録</button>
          </form>

          <form onSubmit={addLogicalPool} className={stepCard}>
            <div className="flex items-center gap-2">
              <span className={stepNumber}>2</span>
              <h4 className="text-sm font-semibold text-gray-900 dark:text-white">Logical Pool</h4>
            </div>
            <p className="text-xs text-gray-500 dark:text-gray-400">認可やセッション要求が参照するクラスタ共通の Pool です。</p>
            <input required className={input} value={poolName} onChange={(event) => setPoolName(event.target.value)} placeholder="linux-standard" />
            <button className={stepButton}>Logical Pool を作成</button>
          </form>

          <form onSubmit={addSupplier} className={stepCard}>
            <div className="flex items-center gap-2">
              <span className={stepNumber}>3</span>
              <h4 className="text-sm font-semibold text-gray-900 dark:text-white">Pool Supplier</h4>
            </div>
            <p className="text-xs text-gray-500 dark:text-gray-400">Manager が供給する Logical Pool とキャパシティを設定します。</p>
            <select required className={input} value={managerID} onChange={(event) => setManagerID(event.target.value)}>
              <option value="" disabled>Manager を選択</option>
              {managers.map((manager) => <option key={manager.id} value={manager.id}>{manager.name}</option>)}
            </select>
            <select required className={input} value={supplierPool} onChange={(event) => setSupplierPool(event.target.value)}>
              <option value="" disabled>Logical Pool を選択</option>
              {pools.map((pool) => <option key={pool.name} value={pool.name}>{pool.name}</option>)}
            </select>
            <div className="grid grid-cols-2 gap-3">
              <label className="text-xs text-gray-500 dark:text-gray-400">Min idle<input required min={0} type="number" className={input} value={minIdle} onChange={(event) => setMinIdle(Number(event.target.value))} /></label>
              <label className="text-xs text-gray-500 dark:text-gray-400">Max runners<input required min={0} type="number" className={input} value={maxRunners} onChange={(event) => setMaxRunners(Number(event.target.value))} /></label>
            </div>
            <button disabled={!managerID || !supplierPool} className={stepButton}>Supplier を追加</button>
          </form>

          <form onSubmit={addBinding} className={stepCard}>
            <div className="flex items-center gap-2">
              <span className={stepNumber}>4</span>
              <h4 className="text-sm font-semibold text-gray-900 dark:text-white">Pool Binding</h4>
            </div>
            <p className="text-xs text-gray-500 dark:text-gray-400">Logical Pool を利用できる user、team、またはクラスタ内の全員を指定します。</p>
            <select required className={input} value={bindingPool} onChange={(event) => setBindingPool(event.target.value)}>
              <option value="" disabled>Logical Pool を選択</option>
              {pools.map((pool) => <option key={pool.name} value={pool.name}>{pool.name}</option>)}
            </select>
            <select className={input} value={subjectType} onChange={(event) => {
              const nextType = event.target.value as 'user' | 'team' | 'all'
              setSubjectType(nextType)
              if (nextType === 'all') {
                setSubjectID('')
                setBindingRole('use')
              }
            }}>
              <option value="team">Team</option>
              <option value="user">User</option>
              <option value="all">All users and teams</option>
            </select>
            <select className={input} value={bindingRole} onChange={(event) => setBindingRole(event.target.value as 'use' | 'manage')}>
              <option value="use">Use</option>
              <option value="manage" disabled={subjectType === 'all'}>Manage</option>
            </select>
            <input required={subjectType !== 'all'} disabled={subjectType === 'all'} className={input} value={subjectID} onChange={(event) => setSubjectID(event.target.value)} placeholder={subjectType === 'all' ? 'Subject ID は不要です' : subjectType === 'team' ? 'org/team' : 'user-id'} />
            <div className="grid grid-cols-2 gap-3">
              <label className="text-xs text-gray-500 dark:text-gray-400">Priority<input type="number" className={input} value={bindingPriority} onChange={(event) => setBindingPriority(Number(event.target.value))} /></label>
              <label className="text-xs text-gray-500 dark:text-gray-400">Max concurrent<input min={0} type="number" className={input} value={maxConcurrent} onChange={(event) => setMaxConcurrent(Number(event.target.value))} /></label>
            </div>
            <button disabled={!bindingPool} className={stepButton}>Binding を追加</button>
          </form>
        </div>
      </SettingsSubsection>

      <SettingsSubsection title="Session Managers">
        <ItemList>
          {managers.length === 0 && <ItemListEmpty>Session Manager はまだありません</ItemListEmpty>}
          {managers.map((manager) => (
            <ItemListRow
              key={manager.id}
              name={manager.name}
              meta={manager.id}
              actions={
                <RowAction tone="danger" onClick={() => void removeManager(manager)} title={`${manager.name}を削除`}>
                  削除
                </RowAction>
              }
            />
          ))}
        </ItemList>
      </SettingsSubsection>

      <SettingsSubsection title="Logical Pools" description="Pool ごとの供給元と利用権限">
        <ItemList>
          {pools.length === 0 && <ItemListEmpty>Logical Pool はまだありません</ItemListEmpty>}
          {pools.map((pool) => {
            const poolSuppliers = suppliers.filter((supplier) => supplier.pool === pool.name)
            const poolBindings = bindings[pool.name] || []
            return (
              <ItemListRow
                key={pool.name}
                name={pool.name}
                actions={
                  <RowAction tone="danger" onClick={() => void removePool(pool)} title={`${pool.name}を削除`}>
                    削除
                  </RowAction>
                }
              >
                <div className="mt-3 grid gap-4 sm:grid-cols-2">
                  <div>
                    <p className="text-xs font-semibold text-gray-500 dark:text-gray-400">Suppliers</p>
                    {poolSuppliers.length === 0 && <p className="mt-1 text-xs text-gray-400">未設定</p>}
                    {poolSuppliers.map((supplier) => (
                      <div key={supplier.manager_id} className="mt-1 flex items-center gap-2 text-xs text-gray-700 dark:text-gray-300">
                        <span>{managers.find((manager) => manager.id === supplier.manager_id)?.name || supplier.manager_id}</span>
                        <span className="text-gray-500 dark:text-gray-400">idle {supplier.idle_runners || 0} / max {supplier.max_runners || '∞'}</span>
                        <button
                          type="button"
                          aria-label={`${supplier.pool}のSupplierを削除`}
                          className="text-red-600 hover:underline dark:text-red-400"
                          onClick={() => void removeSupplier(supplier)}
                        >
                          削除
                        </button>
                      </div>
                    ))}
                  </div>
                  <div>
                    <p className="text-xs font-semibold text-gray-500 dark:text-gray-400">Bindings</p>
                    {poolBindings.length === 0 && <p className="mt-1 text-xs text-gray-400">未設定</p>}
                    <div className="mt-1 flex flex-wrap gap-1.5">
                      {poolBindings.map((binding) => (
                        <span key={binding.id} className="inline-flex items-center gap-1.5 rounded-md bg-gray-100 px-2 py-1 text-xs text-gray-700 dark:bg-gray-700 dark:text-gray-200">
                          {binding.subject_type}: {binding.subject_id || 'everyone'} ({binding.role || 'use'}, priority {binding.priority || 0}, max {binding.max_concurrent || '∞'})
                          <button
                            type="button"
                            aria-label={`${binding.subject_id}のBindingを削除`}
                            className="text-red-600 dark:text-red-400"
                            onClick={() => void removeBinding(pool.name, binding.id)}
                          >
                            ×
                          </button>
                        </span>
                      ))}
                    </div>
                  </div>
                </div>
              </ItemListRow>
            )
          })}
        </ItemList>
      </SettingsSubsection>
    </>
  )
}
