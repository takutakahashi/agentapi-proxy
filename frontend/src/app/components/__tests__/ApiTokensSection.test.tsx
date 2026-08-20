/**
 * @vitest-environment happy-dom
 *
 * Tests for the API Tokens settings section. These assert the *canonical*
 * agentapi-proxy API contract (spec/openapi.json):
 *
 *   GET    /api-tokens?scope=personal|team[&team_id=...]   -> { items: [...] }
 *   POST   /api-tokens                                    -> { token, plaintext_token }
 *   DELETE /api-tokens/:id                                -> 204
 *
 * - scope vocabulary is `personal` | `team` (never `user`).
 * - permissions enum is `session:create | session:read | session:update
 *   | session:delete | admin` (never `session:list`, `session:access`, `*`).
 * - list items expose `token_prefix`, `permissions`, `created_by`, `created_at`.
 * - create returns the plaintext secret exactly once in `plaintext_token`.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, within, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import ApiTokensSection from '../ApiTokensSection'
import ApiTokenResultModal from '../ApiTokenResultModal'
import { ApiToken, CreateApiTokenResponse, CreateApiTokenRequest } from '../../../types/api_token'
import { ToastProvider } from '@/contexts/ToastContext'

// --- Mock the proxy client -------------------------------------------------
const fakeClient = {
  getApiTokens: vi.fn(),
  createApiToken: vi.fn(),
  getApiToken: vi.fn(),
  deleteApiToken: vi.fn(),
}

vi.mock('@/lib/agentapi-proxy-client', () => ({
  createAgentAPIProxyClientFromStorage: () => fakeClient,
  AgentAPIProxyError: class AgentAPIProxyError extends Error {
    status: number
    code: string
    constructor(status: number, code: string, message: string) {
      super(message)
      this.status = status
      this.code = code
      this.name = 'AgentAPIProxyError'
    }
  },
}))

// --- Mock clipboard ---------------------------------------------------------
const clipboardWrite = vi.fn()
beforeEach(() => {
  Object.defineProperty(navigator, 'clipboard', {
    value: { writeText: clipboardWrite },
    configurable: true,
  })
})

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

// Helper: render the section. ToastProvider is required by the section.
function renderSection(props: Partial<React.ComponentProps<typeof ApiTokensSection>> = {}) {
  return render(
    <ToastProvider>
      <ApiTokensSection scope="personal" {...props} />
    </ToastProvider>
  )
}

// A token matching the canonical APITokenMetadata schema (no obsolete perms).
const sampleToken: ApiToken = {
  id: 'tok_1',
  name: 'CI 用',
  token_prefix: 'ap_user_abc',
  scope: 'personal',
  user_id: 'user-1',
  permissions: ['session:create', 'session:read'],
  created_by: 'user-1',
  created_at: '2024-06-14T00:00:00Z',
  updated_at: '2024-06-14T00:00:00Z',
  expires_at: '2025-06-14T00:00:00Z',
}

const teamToken: ApiToken = {
  id: 'tok_team_1',
  name: 'チーム用',
  token_prefix: 'ap_team_xyz',
  scope: 'team',
  user_id: 'svcacct-team-1',
  team_id: 'org/team',
  permissions: ['session:read', 'session:update', 'admin'],
  created_by: 'admin-user',
  created_at: '2024-07-01T00:00:00Z',
  updated_at: '2024-07-01T00:00:00Z',
  expires_at: null,
}

describe('ApiTokensSection — list (GET /api-tokens)', () => {
  it('GET /api-tokens?scope=personal returns {items:[...]} and renders them', async () => {
    fakeClient.getApiTokens.mockResolvedValue({ items: [sampleToken] })
    renderSection({ scope: 'personal' })

    await waitFor(() => expect(screen.getByText('CI 用')).toBeInTheDocument())
    // token_prefix is always rendered (required field in the contract)
    expect(screen.getByText('ap_user_abc')).toBeInTheDocument()
    // canonical permissions are shown
    expect(screen.getByText('session:create')).toBeInTheDocument()
    expect(screen.getByText('session:read')).toBeInTheDocument()
    // obsolete permissions must never appear in the rendered permissions
    expect(screen.queryByText('session:list')).not.toBeInTheDocument()
    expect(screen.queryByText('session:access')).not.toBeInTheDocument()
    expect(screen.queryByText('*')).not.toBeInTheDocument()
    // exact query params: personal scope, no team_id
    expect(fakeClient.getApiTokens).toHaveBeenCalledWith({ scope: 'personal' })
  })

  it('GET /api-tokens?scope=team&team_id=... returns team tokens with created_by', async () => {
    fakeClient.getApiTokens.mockResolvedValue({ items: [teamToken] })
    renderSection({ scope: 'team', teamId: 'org/team' })

    await waitFor(() => expect(screen.getByText('チーム用')).toBeInTheDocument())
    expect(screen.getByText('ap_team_xyz')).toBeInTheDocument()
    // team tokens show the creator
    expect(screen.getByText('作成者: admin-user')).toBeInTheDocument()
    // canonical perms incl. session:update and admin
    expect(screen.getByText('session:update')).toBeInTheDocument()
    expect(screen.getByText('admin')).toBeInTheDocument()
    expect(fakeClient.getApiTokens).toHaveBeenCalledWith({ scope: 'team', team_id: 'org/team' })
  })

  it('shows empty state when items is []', async () => {
    fakeClient.getApiTokens.mockResolvedValue({ items: [] })
    renderSection()
    await waitFor(() => expect(screen.getByText('API トークンがありません')).toBeInTheDocument())
  })

  it('shows error on load failure', async () => {
    fakeClient.getApiTokens.mockRejectedValue(new Error('boom'))
    renderSection()
    await waitFor(() => expect(screen.getByText('boom')).toBeInTheDocument())
  })

  it('team section is fixed to selected team and has no scope switcher', async () => {
    fakeClient.getApiTokens.mockResolvedValue({ items: [] })
    renderSection({ scope: 'team', teamId: 'org/team' })
    await waitFor(() => expect(fakeClient.getApiTokens).toHaveBeenCalledWith({ scope: 'team', team_id: 'org/team' }))
    expect(screen.queryByRole('combobox')).not.toBeInTheDocument()
  })
})

describe('ApiTokensSection — create (POST /api-tokens)', () => {
  it('POST /api-tokens sends canonical scope=personal and renders {token, plaintext_token} once', async () => {
    const user = userEvent.setup()
    fakeClient.getApiTokens.mockResolvedValue({ items: [] })
    const created: CreateApiTokenResponse = {
      token: { ...sampleToken, id: 'tok_new', name: 'My Token' },
      plaintext_token: 'ap_secret_plaintext_123',
    }
    fakeClient.createApiToken.mockResolvedValue(created)

    renderSection()
    await waitFor(() => expect(screen.queryByText('API トークンがありません')).toBeInTheDocument())

    // Open create modal
    await user.click(screen.getByText('トークンを作成'))

    // Fill form
    const nameInput = await screen.findByPlaceholderText('例: CI 用トークン')
    await user.type(nameInput, 'My Token')

    // Select a canonical permission (session:read) — not session:list/access.
    await user.click(screen.getByText('セッション読み取り (session:read)'))

    // Submit
    await user.click(screen.getByRole('button', { name: '作成' }))

    // Result modal shows plaintext exactly once
    const plaintext = await screen.findByTestId('api-token-plaintext')
    expect(plaintext).toHaveTextContent('ap_secret_plaintext_123')
    expect(screen.getByText(/今度一度だけ/)).toBeInTheDocument()

    // Exact create request: canonical scope, no team_id for personal, only
    // the selected canonical permission, and an ISO-8601 expires_at.
    expect(fakeClient.createApiToken).toHaveBeenCalledTimes(1)
    const req = fakeClient.createApiToken.mock.calls[0][0] as CreateApiTokenRequest
    expect(req).toEqual(
      expect.objectContaining({
        name: 'My Token',
        scope: 'personal',
        permissions: ['session:read'],
      })
    )
    expect(req.team_id).toBeUndefined()
    expect(req.expires_at).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}.\d{3}Z$/)

    // List refreshed after create
    await waitFor(() => expect(fakeClient.getApiTokens).toHaveBeenCalledTimes(2))

    // Close result modal -> plaintext disappears
    await user.click(screen.getByText('閉じる（トークンを非表示）'))
    await waitFor(() => expect(screen.queryByTestId('api-token-plaintext')).not.toBeInTheDocument())
    expect(screen.queryByText('ap_secret_plaintext_123')).not.toBeInTheDocument()
  })

  it('POST with team scope includes team_id and omits permissions when none selected', async () => {
    const user = userEvent.setup()
    fakeClient.getApiTokens.mockResolvedValue({ items: [] })
    fakeClient.createApiToken.mockResolvedValue({
      token: { ...teamToken, id: 'tok_team_new', name: 'T' },
      plaintext_token: 'ap_team_secret',
    })
    renderSection({ scope: 'team', teamId: 'org/team' })

    await user.click(await screen.findByText('トークンを作成'))
    await user.type(screen.getByPlaceholderText('例: CI 用トークン'), 'T')
    // No permission checkbox selected -> permissions omitted (defaults apply).
    await user.click(screen.getByRole('button', { name: '作成' }))

    await screen.findByTestId('api-token-plaintext')
    const req = fakeClient.createApiToken.mock.calls[0][0] as CreateApiTokenRequest
    expect(req).toEqual(
      expect.objectContaining({ name: 'T', scope: 'team', team_id: 'org/team' })
    )
    expect(req.permissions).toBeUndefined()
  })

  it('copies plaintext token to clipboard', async () => {
    const user = userEvent.setup()
    fakeClient.getApiTokens.mockResolvedValue({ items: [] })
    fakeClient.createApiToken.mockResolvedValue({
      token: { ...sampleToken, name: 'C' },
      plaintext_token: 'ap_copy_me',
    })
    clipboardWrite.mockResolvedValue(undefined)
    renderSection()
    await user.click(await screen.findByText('トークンを作成'))
    await user.type(screen.getByPlaceholderText('例: CI 用トークン'), 'C')
    await user.click(screen.getByRole('button', { name: '作成' }))
    await screen.findByTestId('api-token-plaintext')

    await user.click(screen.getByRole('button', { name: /コピー/ }))
    await waitFor(() => expect(screen.getByText('コピー済み')).toBeInTheDocument())
  })

  it('does not persist plaintext token in localStorage', async () => {
    const user = userEvent.setup()
    const setItemSpy = vi.spyOn(Storage.prototype, 'setItem')
    fakeClient.getApiTokens.mockResolvedValue({ items: [] })
    fakeClient.createApiToken.mockResolvedValue({
      token: { ...sampleToken, name: 'X' },
      plaintext_token: 'ap_persist_test',
    })
    renderSection()
    await user.click(await screen.findByText('トークンを作成'))
    await user.type(screen.getByPlaceholderText('例: CI 用トークン'), 'X')
    await user.click(screen.getByRole('button', { name: '作成' }))
    await screen.findByTestId('api-token-plaintext')

    for (const call of setItemSpy.mock.calls) {
      expect(JSON.stringify(call[1])).not.toContain('ap_persist_test')
    }
    setItemSpy.mockRestore()
  })

  it('validates name length (1-64)', async () => {
    const user = userEvent.setup()
    fakeClient.getApiTokens.mockResolvedValue({ items: [] })
    renderSection()
    await user.click(await screen.findByText('トークンを作成'))

    expect(screen.getByRole('button', { name: '作成' })).toBeDisabled()

    await user.type(screen.getByPlaceholderText('例: CI 用トークン'), 'ab')
    expect(screen.getByRole('button', { name: '作成' })).toBeEnabled()
  })

  it('offers only canonical permission labels (no session:list/session:access/*)', async () => {
    const user = userEvent.setup()
    fakeClient.getApiTokens.mockResolvedValue({ items: [] })
    renderSection()
    await user.click(await screen.findByText('トークンを作成'))

    const labels = ['session:create', 'session:read', 'session:update', 'session:delete', 'admin']
    for (const l of labels) {
      expect(screen.getByText(new RegExp(l))).toBeInTheDocument()
    }
    // The obsolete wildcard-permission label ('すべての操作 (*)') must not be offered.
    expect(screen.queryByText(/すべての操作/)).not.toBeInTheDocument()
    // Obsolete permission names must never appear as selectable labels.
    expect(screen.queryByText(/session:list/)).not.toBeInTheDocument()
    expect(screen.queryByText(/session:access/)).not.toBeInTheDocument()
  })
})

describe('ApiTokensSection — delete (DELETE /api-tokens/:id)', () => {
  it('confirms destructive deletion and refreshes the list', async () => {
    const user = userEvent.setup()
    fakeClient.getApiTokens.mockResolvedValue({ items: [sampleToken] })
    fakeClient.deleteApiToken.mockResolvedValue(undefined)
    renderSection()
    await screen.findByText('CI 用')

    await user.click(screen.getByRole('button', { name: /トークン CI 用 を削除/ }))
    expect(screen.getByText(/API トークンを削除します/)).toBeInTheDocument()
    const dialog = screen.getByText(/API トークンを削除します/).closest('.fixed') as HTMLElement
    await user.click(within(dialog).getByRole('button', { name: '削除' }))

    expect(fakeClient.deleteApiToken).toHaveBeenCalledWith('tok_1')
    await waitFor(() => expect(fakeClient.getApiTokens.mock.calls.length).toBeGreaterThanOrEqual(2))
  })

  it('cancels deletion', async () => {
    const user = userEvent.setup()
    fakeClient.getApiTokens.mockResolvedValue({ items: [sampleToken] })
    renderSection()
    await screen.findByText('CI 用')

    await user.click(screen.getByRole('button', { name: /トークン CI 用 を削除/ }))
    const dialog = screen.getByText(/API トークンを削除します/).closest('.fixed') as HTMLElement
    await user.click(within(dialog).getByRole('button', { name: 'キャンセル' }))

    await waitFor(() => expect(screen.queryByText(/API トークンを削除します/)).not.toBeInTheDocument())
    expect(fakeClient.deleteApiToken).not.toHaveBeenCalled()
  })

  it('shows delete error without crashing', async () => {
    const user = userEvent.setup()
    fakeClient.getApiTokens.mockResolvedValue({ items: [sampleToken] })
    fakeClient.deleteApiToken.mockRejectedValue(new Error('forbidden'))
    renderSection()
    await screen.findByText('CI 用')

    await user.click(screen.getByRole('button', { name: /トークン CI 用 を削除/ }))
    const dialog = screen.getByText(/API トークンを削除します/).closest('.fixed') as HTMLElement
    await user.click(within(dialog).getByRole('button', { name: '削除' }))

    await waitFor(() => expect(screen.getByText('forbidden')).toBeInTheDocument())
  })
})

describe('ApiTokenResultModal (one-time display)', () => {
  it('renders nothing when response is null', () => {
    const { container } = render(<ApiTokenResultModal response={null} onClose={() => {}} />)
    expect(container).toBeEmptyDOMElement()
  })

  it('clears plaintext state on close', async () => {
    const user = userEvent.setup()
    const onClose = vi.fn()
    const response: CreateApiTokenResponse = {
      token: { ...sampleToken, name: 'Z' },
      plaintext_token: 'ap_clear_me',
    }
    const { unmount } = render(<ApiTokenResultModal response={response} onClose={onClose} />)
    expect(screen.getByTestId('api-token-plaintext')).toHaveTextContent('ap_clear_me')
    await user.click(screen.getByText('閉じる（トークンを非表示）'))
    expect(onClose).toHaveBeenCalled()
    unmount()
  })
})