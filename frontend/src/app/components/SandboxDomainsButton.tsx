'use client';

import { useState, useEffect, useCallback } from 'react';
import { AgentAPIProxyClient, AgentAPIProxyError } from '../../lib/agentapi-proxy-client';
import { SandboxPolicy, UpdateSandboxPolicyRequest } from '../../types/sandbox_policy';

interface SandboxDomainsButtonProps {
  sessionId: string;
  agentAPI: AgentAPIProxyClient;
}

export default function SandboxDomainsButton({ sessionId, agentAPI }: SandboxDomainsButtonProps) {
  const [isOpen, setIsOpen] = useState(false);
  const [isLoading, setIsLoading] = useState(false);
  const [allowed, setAllowed] = useState<string[]>([]);
  const [denied, setDenied] = useState<string[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [policies, setPolicies] = useState<SandboxPolicy[]>([]);
  const [selectedPolicyId, setSelectedPolicyId] = useState('');
  const [selectedDomains, setSelectedDomains] = useState<Set<string>>(new Set());
  const [addSuccess, setAddSuccess] = useState<string | null>(null);
  const [isAdding, setIsAdding] = useState(false);
  const [activeTab, setActiveTab] = useState<'denied' | 'allowed'>('denied');

  const fetchDomains = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    try {
      const result = await agentAPI.getSessionSandboxDomains(sessionId);
      setAllowed(result.allowed ?? []);
      setDenied(result.denied ?? []);
    } catch (err) {
      if (err instanceof AgentAPIProxyError && err.status === 503) {
        setError('このセッションにはネットワークフィルター（サンドボックス）が設定されていません');
      } else if (err instanceof AgentAPIProxyError && err.status === 501) {
        setError('このセッションタイプではサンドボックスドメインの取得はサポートされていません');
      } else {
        setError('ドメインリストの取得に失敗しました');
      }
    } finally {
      setIsLoading(false);
    }
  }, [agentAPI, sessionId]);

  const fetchPolicies = useCallback(async () => {
    try {
      const result = await agentAPI.getSandboxPolicies();
      setPolicies(result.sandbox_policies ?? []);
      if (result.sandbox_policies?.length > 0 && !selectedPolicyId) {
        setSelectedPolicyId(result.sandbox_policies[0].id);
      }
    } catch {
      // ignore — policies are optional for this flow
    }
  }, [agentAPI, selectedPolicyId]);

  useEffect(() => {
    if (isOpen) {
      fetchDomains();
      fetchPolicies();
    }
  }, [isOpen, fetchDomains, fetchPolicies]);

  const toggleDomain = (domain: string) => {
    setSelectedDomains((prev) => {
      const next = new Set(prev);
      if (next.has(domain)) {
        next.delete(domain);
      } else {
        next.add(domain);
      }
      return next;
    });
  };

  const handleAddToPolicy = async () => {
    if (!selectedPolicyId || selectedDomains.size === 0) return;
    setIsAdding(true);
    setAddSuccess(null);
    setError(null);
    try {
      const policy = policies.find((p) => p.id === selectedPolicyId);
      if (!policy) throw new Error('ポリシーが見つかりません');

      const domainsToAdd = Array.from(selectedDomains);
      const isAllowlist = (policy.allowed_domains?.length ?? 0) > 0 || (policy.denied_domains?.length ?? 0) === 0;
      const update: UpdateSandboxPolicyRequest = isAllowlist
        ? { allowed_domains: [...(policy.allowed_domains ?? []), ...domainsToAdd.filter((d) => !policy.allowed_domains?.includes(d))] }
        : { denied_domains: (policy.denied_domains ?? []).filter((d) => !domainsToAdd.includes(d)) };

      await agentAPI.updateSandboxPolicy(selectedPolicyId, update);
      setAddSuccess(`${domainsToAdd.length} 件のドメインをポリシー「${policy.name}」に追加しました`);
      setSelectedDomains(new Set());
    } catch (err) {
      setError(err instanceof AgentAPIProxyError ? err.message : 'ポリシーの更新に失敗しました');
    } finally {
      setIsAdding(false);
    }
  };

  const currentDomains = activeTab === 'denied' ? denied : allowed;

  return (
    <>
      <button
        onClick={() => setIsOpen(true)}
        className="p-2 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-800 rounded-md transition-colors"
        title="サンドボックス アクセスドメイン"
      >
        <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z" />
        </svg>
      </button>

      {isOpen && (
        <div
          className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4"
          onClick={(e) => { if (e.target === e.currentTarget) setIsOpen(false); }}
        >
          <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl w-full max-w-lg max-h-[80vh] flex flex-col">
            {/* Header */}
            <div className="px-6 py-4 border-b border-gray-200 dark:border-gray-700 flex items-center justify-between shrink-0">
              <h2 className="text-lg font-semibold text-gray-900 dark:text-white">
                サンドボックス アクセスドメイン
              </h2>
              <button
                onClick={() => setIsOpen(false)}
                className="text-gray-400 hover:text-gray-500 dark:hover:text-gray-300"
              >
                <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                </svg>
              </button>
            </div>

            {/* Body */}
            <div className="flex-1 overflow-y-auto px-6 py-4 space-y-4">
              {isLoading && (
                <div className="flex items-center justify-center py-8">
                  <svg className="animate-spin w-6 h-6 text-blue-500" fill="none" viewBox="0 0 24 24">
                    <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                    <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z" />
                  </svg>
                </div>
              )}

              {error && !isLoading && (
                <div className="p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-red-700 dark:text-red-400 text-sm">
                  {error}
                </div>
              )}

              {addSuccess && (
                <div className="p-3 bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded-md text-green-700 dark:text-green-400 text-sm">
                  {addSuccess}
                </div>
              )}

              {!isLoading && !error && (
                <>
                  {/* Tab switcher */}
                  <div className="flex border-b border-gray-200 dark:border-gray-700">
                    <button
                      onClick={() => setActiveTab('denied')}
                      className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${
                        activeTab === 'denied'
                          ? 'border-red-500 text-red-600 dark:text-red-400'
                          : 'border-transparent text-gray-500 hover:text-gray-700 dark:text-gray-400'
                      }`}
                    >
                      ブロック済み ({denied.length})
                    </button>
                    <button
                      onClick={() => setActiveTab('allowed')}
                      className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${
                        activeTab === 'allowed'
                          ? 'border-green-500 text-green-600 dark:text-green-400'
                          : 'border-transparent text-gray-500 hover:text-gray-700 dark:text-gray-400'
                      }`}
                    >
                      許可済み ({allowed.length})
                    </button>
                  </div>

                  {/* Domain list */}
                  {currentDomains.length === 0 ? (
                    <p className="text-sm text-gray-500 dark:text-gray-400 py-4 text-center">
                      {activeTab === 'denied' ? 'ブロックされたドメインはありません' : '許可されたドメインはありません'}
                    </p>
                  ) : (
                    <div className="space-y-1 max-h-48 overflow-y-auto">
                      {currentDomains.map((domain) => (
                        <label
                          key={domain}
                          className="flex items-center space-x-3 px-2 py-1.5 rounded hover:bg-gray-50 dark:hover:bg-gray-700 cursor-pointer"
                        >
                          <input
                            type="checkbox"
                            checked={selectedDomains.has(domain)}
                            onChange={() => toggleDomain(domain)}
                            className="rounded border-gray-300 dark:border-gray-600 text-blue-600"
                          />
                          <span className={`text-sm font-mono flex-1 truncate ${
                            activeTab === 'denied' ? 'text-red-600 dark:text-red-400' : 'text-green-600 dark:text-green-400'
                          }`}>
                            {domain}
                          </span>
                        </label>
                      ))}
                    </div>
                  )}

                  {/* Add to policy section */}
                  {selectedDomains.size > 0 && policies.length > 0 && (
                    <div className="border-t border-gray-200 dark:border-gray-700 pt-4 space-y-3">
                      <p className="text-sm font-medium text-gray-700 dark:text-gray-300">
                        {selectedDomains.size} 件のドメインをポリシーに追加
                      </p>
                      <select
                        value={selectedPolicyId}
                        onChange={(e) => setSelectedPolicyId(e.target.value)}
                        className="w-full px-3 py-2 text-sm border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-white"
                      >
                        {policies.map((p) => (
                          <option key={p.id} value={p.id}>{p.name}</option>
                        ))}
                      </select>
                      <button
                        onClick={handleAddToPolicy}
                        disabled={isAdding}
                        className="w-full px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:bg-gray-300 dark:disabled:bg-gray-600 text-white rounded-md transition-colors disabled:cursor-not-allowed text-sm"
                      >
                        {isAdding ? '追加中...' : 'ポリシーに追加（許可リスト更新）'}
                      </button>
                    </div>
                  )}

                  {selectedDomains.size > 0 && policies.length === 0 && (
                    <div className="border-t border-gray-200 dark:border-gray-700 pt-4">
                      <p className="text-sm text-gray-500 dark:text-gray-400">
                        ドメインを追加するには、まず<a href="/sandbox-policies" className="text-blue-600 hover:underline ml-1">サンドボックスポリシー</a>を作成してください。
                      </p>
                    </div>
                  )}
                </>
              )}
            </div>

            {/* Footer */}
            <div className="px-6 py-3 border-t border-gray-200 dark:border-gray-700 shrink-0 flex justify-between items-center">
              <button
                onClick={fetchDomains}
                disabled={isLoading}
                className="text-sm text-blue-600 hover:text-blue-700 dark:text-blue-400 disabled:opacity-50"
              >
                再読み込み
              </button>
              <button
                onClick={() => setIsOpen(false)}
                className="px-4 py-2 text-sm bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600 text-gray-700 dark:text-gray-300 rounded-md transition-colors"
              >
                閉じる
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
