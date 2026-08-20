'use client'

import { ScopeGate } from '../../../sections/ScopeGate'
import { MarketplacesSection } from '../../../sections/MarketplacesSection'

export default function Page() {
  return (
    <ScopeGate>
      <MarketplacesSection />
    </ScopeGate>
  )
}
