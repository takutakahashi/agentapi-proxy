type ResumeResult = { session_id: string; status: string }

export interface SessionResumeClient {
  resumeSession(sessionId: string): Promise<ResumeResult>
}

interface ResumeOptions {
  intervalMs?: number
  maxAttempts?: number
  wait?: (milliseconds: number) => Promise<void>
  cancelled?: () => boolean
}

function throwIfCancelled(cancelled?: () => boolean) {
  if (cancelled?.()) throw new Error('Session restoration cancelled')
}

export async function waitForSessionResume(
  client: SessionResumeClient,
  sessionId: string,
  options: ResumeOptions = {},
): Promise<ResumeResult> {
  const intervalMs = options.intervalMs ?? 1000
  const maxAttempts = options.maxAttempts ?? 120
  const wait = options.wait ?? ((milliseconds: number) => new Promise(resolve => setTimeout(resolve, milliseconds)))

  for (let attempt = 0; attempt < maxAttempts; attempt += 1) {
    throwIfCancelled(options.cancelled)
    const result = await client.resumeSession(sessionId)
    if (result.status !== 'restoring') return result
    if (attempt + 1 < maxAttempts) {
      await wait(intervalMs)
      throwIfCancelled(options.cancelled)
    }
  }

  throw new Error('Timed out waiting for the session workload to resume')
}
