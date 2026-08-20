import { redirect } from 'next/navigation'

export default async function TeamSettingsPage({
  params,
}: {
  params: Promise<{ team: string }>
}) {
  const { team } = await params
  redirect(`/settings/team/${team}/ai-providers`)
}
