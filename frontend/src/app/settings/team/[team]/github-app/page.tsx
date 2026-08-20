'use client'

import { ScopeGate } from '../../../sections/ScopeGate'
import { GitHubAppSection } from '../../../sections/GitHubAppSection'

export default function Page() {
  return (
    <ScopeGate>
      <GitHubAppSection />
    </ScopeGate>
  )
}
