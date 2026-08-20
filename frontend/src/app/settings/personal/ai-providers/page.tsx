'use client'

import { ScopeGate } from '../../sections/ScopeGate'
import { AIProvidersSection } from '../../sections/AIProvidersSection'

export default function Page() {
  return (
    <ScopeGate>
      <AIProvidersSection />
    </ScopeGate>
  )
}
