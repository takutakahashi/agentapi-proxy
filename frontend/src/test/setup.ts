import '@testing-library/jest-dom'
import { beforeEach, vi } from 'vitest'

// Mock Next.js router hooks
vi.mock('next/navigation', () => ({
  useRouter: () => ({
    push: vi.fn(),
    replace: vi.fn(),
    prefetch: vi.fn(),
    back: vi.fn(),
    forward: vi.fn(),
    refresh: vi.fn(),
  }),
  useSearchParams: () => new URLSearchParams(),
  usePathname: () => '/',
  useParams: () => ({}),
  redirect: vi.fn(),
}))

// Global test setup
beforeEach(() => {
  // Reset any mocks or test state if needed
  if (vi.clearAllTimers) {
    vi.clearAllTimers();
  }
})

// Global cleanup after each test to prevent memory leaks
import { afterEach } from 'vitest'
afterEach(() => {
  // Clear any pending timers
  if (vi.clearAllTimers) {
    vi.clearAllTimers();
  }
  
  // Force cleanup of DOM if in jsdom environment
  if (typeof document !== 'undefined') {
    document.body.innerHTML = '';
  }
  
  // Trigger garbage collection if available (mainly for local development)
  if (global.gc) {
    global.gc();
  }
})