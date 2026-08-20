'use client'

import { ScopeGate } from '../../sections/ScopeGate'
import { NotificationsSection } from '../../sections/NotificationsSection'

export default function Page() {
  return (
    <ScopeGate>
      <NotificationsSection />
    </ScopeGate>
  )
}
