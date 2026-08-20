'use client'

import { ScopeGate } from '../../../sections/ScopeGate'
import { PluginsSection } from '../../../sections/PluginsSection'

export default function Page() {
  return (
    <ScopeGate>
      <PluginsSection />
    </ScopeGate>
  )
}
