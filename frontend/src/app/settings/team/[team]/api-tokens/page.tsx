'use client'

import { ScopeGate } from '../../../sections/ScopeGate'
import { ApiTokensPage } from '../../../sections/ApiTokensPage'

export default function Page() {
  return (
    <ScopeGate>
      <ApiTokensPage />
    </ScopeGate>
  )
}
