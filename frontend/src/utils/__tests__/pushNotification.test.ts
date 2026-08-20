import { afterEach, describe, expect, it, vi } from 'vitest'

import { PushNotificationManager } from '../pushNotification'

describe('PushNotificationManager.unsubscribe', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('treats an already-missing browser subscription as a successful reset', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch')
    const manager = new PushNotificationManager()

    await expect(manager.unsubscribe()).resolves.toBe(true)
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('treats an already-missing server subscription as a successful deletion', async () => {
    const unsubscribe = vi.fn().mockResolvedValue(true)
    const manager = new PushNotificationManager()
    Object.assign(manager, {
      subscription: {
        endpoint: 'https://push.example.test/subscription',
        unsubscribe,
      },
    })
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(null, { status: 404 }))

    await expect(manager.unsubscribe()).resolves.toBe(true)
    expect(unsubscribe).toHaveBeenCalledOnce()
    expect(manager.getSubscriptionStatus().isSubscribed).toBe(false)
  })
})
