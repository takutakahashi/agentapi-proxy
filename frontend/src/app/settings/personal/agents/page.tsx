'use client'

import { ScopeGate } from '../../sections/ScopeGate'
import { AgentsSection } from '../../sections/AgentsSection'

export default function Page() {
  return (
    <ScopeGate>
      <AgentsSection />
    </ScopeGate>
  )
}
