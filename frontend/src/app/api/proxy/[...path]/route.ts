import { createProxyRouteHandler } from '@/lib/server-proxy-route'

const options = {
  publicPrefix: '/api/proxy',
  passThroughAuthorization: true,
}

export const GET = createProxyRouteHandler('GET', options)
export const POST = createProxyRouteHandler('POST', options)
export const PUT = createProxyRouteHandler('PUT', options)
export const DELETE = createProxyRouteHandler('DELETE', options)
export const PATCH = createProxyRouteHandler('PATCH', options)
