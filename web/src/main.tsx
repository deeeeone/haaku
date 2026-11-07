import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRouter, RouterProvider } from '@tanstack/react-router'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { Toaster } from 'sonner'
import { AuthProvider } from '@/auth/provider'
import './index.css'

import { routeTree } from './routeTree.gen'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      retry: 2,
    },
  },
})

const router = createRouter({
  routeTree,
  context: { queryClient },
  defaultPreload: 'intent',
})

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}

const root = document.getElementById('root')
if (!root) throw new Error('Root element not found')

// Mode discovery happens inside <AuthProvider>: it calls GET /v1/auth/config
// first and branches — `mode:"clerk"` mounts the ClerkProvider (today's path),
// `mode:"local"` mounts the local flow with Clerk inert.
const app = (
  <QueryClientProvider client={queryClient}>
    <AuthProvider>
      <RouterProvider router={router} />
      <Toaster
        theme="dark"
        position="bottom-right"
        toastOptions={{
          style: {
            background: 'rgba(255, 255, 255, 0.11)',
            border: '1px solid rgba(255, 255, 255, 0.13)',
            color: '#f8fbff',
            borderRadius: '18px',
          },
        }}
      />
    </AuthProvider>
  </QueryClientProvider>
)

createRoot(root).render(<StrictMode>{app}</StrictMode>)
