'use client'

import { SettingsData } from '@/types/settings'
import { FieldGroup, FieldRow, SelectField, SettingsPageHeader } from '@/components/settings'
import { useSettingsScope } from '../SettingsScopeContext'

const agentOptions = [
  { value: 'auto', label: '自動選択' },
  { value: 'claude-acp', label: 'Claude ACP' },
  { value: 'codex-acp', label: 'Codex ACP' },
  { value: 'pi-ollama', label: 'Pi Ollama' },
  { value: 'cursor', label: 'Cursor ACP' },
]

export function AgentsSection() {
  const { scopeKind, settings, update } = useSettingsScope()

  return (
    <>
      <SettingsPageHeader
        title="エージェント"
        description="セッション作成時にエージェントの指定がなかった場合の動作を決めます。"
      />

      <FieldGroup>
        <FieldRow
          label="デフォルトエージェント"
          htmlFor="default-agent-type"
          description={
            scopeKind === 'personal'
              ? 'パーソナルセッションでエージェントを指定しなかったときに使われます'
              : 'チームのセッションでエージェントを指定しなかったときに使われます'
          }
          control={
            <SelectField
              id="default-agent-type"
              value={settings.default_agent_type || 'auto'}
              onChange={(value) =>
                update({
                  default_agent_type: value as NonNullable<SettingsData['default_agent_type']>,
                })
              }
              options={agentOptions}
            />
          }
        />
      </FieldGroup>
    </>
  )
}
