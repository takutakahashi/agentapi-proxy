import { describe, expect, it, vi } from 'vitest'
import { waitForSessionResume } from '../session-resume'

describe('waitForSessionResume', () => {
  it('waits until restoration completes before opening the chat', async () => {
    const resumeSession = vi.fn()
      .mockResolvedValueOnce({ session_id: 'session-1', status: 'restoring' })
      .mockResolvedValueOnce({ session_id: 'session-1', status: 'restoring' })
      .mockResolvedValueOnce({ session_id: 'session-1', status: 'active' })
    const wait = vi.fn().mockResolvedValue(undefined)

    await expect(waitForSessionResume(
      { resumeSession },
      'session-1',
      { intervalMs: 10, maxAttempts: 3, wait },
    )).resolves.toEqual({ session_id: 'session-1', status: 'active' })

    expect(resumeSession).toHaveBeenCalledTimes(3)
    expect(wait).toHaveBeenCalledTimes(2)
  })

  it('fails when restoration never completes', async () => {
    const resumeSession = vi.fn().mockResolvedValue({ session_id: 'session-1', status: 'restoring' })

    await expect(waitForSessionResume(
      { resumeSession },
      'session-1',
      { maxAttempts: 2, wait: async () => undefined },
    )).rejects.toThrow('Timed out waiting for the session workload to resume')
  })

  it('stops polling after the chat screen is left', async () => {
    const resumeSession = vi.fn().mockResolvedValue({ session_id: 'session-1', status: 'restoring' })
    let cancelled = false

    await expect(waitForSessionResume(
      { resumeSession },
      'session-1',
      {
        cancelled: () => cancelled,
        wait: async () => { cancelled = true },
      },
    )).rejects.toThrow('Session restoration cancelled')
    expect(resumeSession).toHaveBeenCalledTimes(1)
  })
})
