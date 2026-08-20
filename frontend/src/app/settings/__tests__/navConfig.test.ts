import { describe, expect, it } from 'vitest'
import {
  DEFAULT_SETTINGS_SLUG,
  navItemsForScope,
  pagesForDirtyFields,
  settingsHref,
} from '../navConfig'

describe('navItemsForScope', () => {
  it('keeps personal-only pages out of the team sidebar', () => {
    const teamSlugs = navItemsForScope('team').map((item) => item.slug)
    expect(teamSlugs).not.toContain('notifications')
    expect(teamSlugs).not.toContain('files')
  })

  it('keeps team-only pages out of the personal sidebar', () => {
    const personalSlugs = navItemsForScope('personal').map((item) => item.slug)
    expect(personalSlugs).not.toContain('github-app')
  })

  it('offers the default page in both scopes so scope switching can fall back to it', () => {
    for (const scope of ['personal', 'team'] as const) {
      expect(navItemsForScope(scope).map((item) => item.slug)).toContain(DEFAULT_SETTINGS_SLUG)
    }
  })
})

describe('settingsHref', () => {
  it('builds personal routes', () => {
    expect(settingsHref('personal', 'mcp-servers')).toBe('/settings/personal/mcp-servers')
  })

  it('encodes the team name so org/team slugs stay in one path segment', () => {
    expect(settingsHref('team', 'mcp-servers', 'acme/dev')).toBe(
      '/settings/team/acme%2Fdev/mcp-servers'
    )
  })
})

describe('pagesForDirtyFields', () => {
  it('names every page holding a changed field', () => {
    expect(pagesForDirtyFields('personal', ['mcp_servers', 'slack_user_id'])).toEqual([
      '通知',
      'MCP サーバー',
    ])
  })

  it('ignores fields that no page in the scope owns', () => {
    // github_app_installation_id はチームにしか無い
    expect(pagesForDirtyFields('personal', ['github_app_installation_id'])).toEqual([])
    expect(pagesForDirtyFields('team', ['github_app_installation_id'])).toEqual([
      'GitHub App 認証',
    ])
  })

  it('reports nothing when there are no changes', () => {
    expect(pagesForDirtyFields('personal', [])).toEqual([])
  })
})
