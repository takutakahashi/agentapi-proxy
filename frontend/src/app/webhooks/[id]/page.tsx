'use client'

import { useState, useEffect, use } from 'react'
import { useRouter } from 'next/navigation'
import { Webhook } from '../../../types/webhook'
import { createAgentAPIProxyClientFromStorage } from '../../../lib/agentapi-proxy-client'
import TopBar from '../../components/TopBar'
import NavigationTabs from '../../components/NavigationTabs'
import WebhookTriggerTest from '../../components/WebhookTriggerTest'
import LoadingSpinner from '../../components/LoadingSpinner'

interface WebhookDetailPageProps {
  params: Promise<{ id: string }>
}

export default function WebhookDetailPage({ params }: WebhookDetailPageProps) {
  const resolvedParams = use(params)
  const router = useRouter()
  const [webhook, setWebhook] = useState<Webhook | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const fetchWebhook = async () => {
      try {
        const client = createAgentAPIProxyClientFromStorage()
        const webhookData = await client.getWebhook(resolvedParams.id)
        setWebhook(webhookData)
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to load webhook')
      } finally {
        setIsLoading(false)
      }
    }

    fetchWebhook()
  }, [resolvedParams.id])

  const handleBack = () => {
    router.push('/webhooks')
  }

  if (isLoading) {
    return (
      <main className="min-h-dvh bg-gray-50 dark:bg-gray-900">
        <TopBar title="Webhook Test" showSettingsButton={true}>
          <div className="md:hidden">
            <NavigationTabs />
          </div>
        </TopBar>
        <div className="flex items-center justify-center h-[calc(100vh-4rem)]">
          <LoadingSpinner />
        </div>
      </main>
    )
  }

  if (error || !webhook) {
    return (
      <main className="min-h-dvh bg-gray-50 dark:bg-gray-900">
        <TopBar title="Webhook Test" showSettingsButton={true}>
          <div className="md:hidden">
            <NavigationTabs />
          </div>
        </TopBar>
        <div className="flex items-center justify-center h-[calc(100vh-4rem)]">
          <div className="text-center">
            <p className="text-red-600 dark:text-red-400 mb-4">{error || 'Webhook not found'}</p>
            <button
              onClick={handleBack}
              className="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white rounded-lg"
            >
              Back to Webhooks
            </button>
          </div>
        </div>
      </main>
    )
  }

  return (
    <main className="min-h-dvh bg-gray-50 dark:bg-gray-900">
      <TopBar title="Webhook Test" showSettingsButton={true}>
        <div className="md:hidden">
          <NavigationTabs />
        </div>
      </TopBar>

      <div className="px-4 md:px-6 lg:px-8 pt-6 md:pt-8 pb-6 md:pb-8 max-w-6xl mx-auto">
        {/* Back Button */}
        <button
          onClick={handleBack}
          className="mb-6 flex items-center text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-white"
        >
          <svg className="w-5 h-5 mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
          </svg>
          Back to Webhooks
        </button>

        {/* Trigger Test */}
        <WebhookTriggerTest webhook={webhook} />
      </div>
    </main>
  )
}
