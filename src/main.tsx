import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { ClerkProvider } from '@clerk/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  RouterProvider,
  createRouter,
} from '@tanstack/react-router'
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
const clerkConfigured =
  clerkPubKey && !clerkPubKey.includes('YOUR_CLERK')

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
      <ClerkProvider
        publishableKey={clerkPubKey}
        proxyUrl="http://localhost:4000"
        appearance={{
          variables: {
            colorPrimary: '#8c6bff',
            colorBackground: '#0c1122',
            colorText: '#f8fbff',
            colorTextSecondary: 'rgba(248, 251, 255, 0.66)',
            colorInputBackground: 'rgba(255, 255, 255, 0.072)',
            colorInputText: '#f8fbff',
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
