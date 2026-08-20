'use client'

import { ScopeGate } from '../../../sections/ScopeGate'
import { CodexAuthSection } from '../../../sections/CodexAuthSection'

export default function Page() {
  return (
    <ScopeGate>
      <CodexAuthSection />
    </ScopeGate>
  )
}
