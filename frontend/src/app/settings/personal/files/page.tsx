'use client'

import { ScopeGate } from '../../sections/ScopeGate'
import { FilesSection } from '../../sections/FilesSection'

export default function Page() {
  return (
    <ScopeGate>
      <FilesSection />
    </ScopeGate>
  )
}
