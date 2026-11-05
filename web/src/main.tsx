import { ClerkProvider } from '@clerk/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRouter, RouterProvider } from '@tanstack/react-router'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { Toaster } from 'sonner'
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

const clerkPubKey = import.meta.env.VITE_CLERK_PUBLISHABLE_KEY
const clerkConfigured = clerkPubKey && !clerkPubKey.includes('YOUR_CLERK')

// Optional Clerk proxy (FAPI proxy / self-hosted frontend API). The dev stack's
// `clerk-emulator` runs on :4000, but that host does not exist in production —
// so only pass a proxy when the build actually configures one. Absent a value,
// Clerk talks to its own hosted frontend API, as it does for every self-hoster
// on a real Clerk tenant.
const clerkProxyUrl = import.meta.env.VITE_CLERK_PROXY_URL || undefined

const root = document.getElementById('root')
if (!root) throw new Error('Root element not found')

const app = (
  <QueryClientProvider client={queryClient}>
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
  </QueryClientProvider>
)

createRoot(root).render(
  <StrictMode>
    {clerkConfigured ? (
      // `afterSignOutUrl` moved from <UserButton> to the provider in Clerk v6.
      <ClerkProvider
        publishableKey={clerkPubKey}
        proxyUrl={clerkProxyUrl}
        afterSignOutUrl="/"
        appearance={{
          variables: {
            colorPrimary: '#8c6bff',
            colorBackground: '#0c1122',
            colorForeground: '#f8fbff',
            colorMutedForeground: 'rgba(248, 251, 255, 0.66)',
            colorInput: 'rgba(255, 255, 255, 0.072)',
            colorInputForeground: '#f8fbff',
            borderRadius: '18px',
          },
        }}
      >
        {app}
      </ClerkProvider>
    ) : (
      app
    )}
  </StrictMode>
)
