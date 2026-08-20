'use client'

import { ScopeGate } from '../../sections/ScopeGate'
import { McpServersSection } from '../../sections/McpServersSection'

export default function Page() {
  return (
    <ScopeGate>
      <McpServersSection />
    </ScopeGate>
  )
}
