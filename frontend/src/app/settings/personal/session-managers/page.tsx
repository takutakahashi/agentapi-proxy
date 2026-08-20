'use client'

import { ScopeGate } from '../../sections/ScopeGate'
import { SessionManagersSection } from '../../sections/SessionManagersSection'

export default function Page() {
  return (
    <ScopeGate>
      <SessionManagersSection />
    </ScopeGate>
  )
}
