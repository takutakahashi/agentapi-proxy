import { SettingsShell } from './SettingsShell'

export default function SettingsLayout({
  children,
}: {
  children: React.ReactNode
}) {
  return <SettingsShell>{children}</SettingsShell>
}
