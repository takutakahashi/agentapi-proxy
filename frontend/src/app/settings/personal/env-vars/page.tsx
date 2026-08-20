'use client'

import { ScopeGate } from '../../sections/ScopeGate'
import { EnvVarsSection } from '../../sections/EnvVarsSection'

export default function Page() {
  return (
    <ScopeGate>
      <EnvVarsSection />
    </ScopeGate>
  )
}
