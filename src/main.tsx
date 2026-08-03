import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { ClerkProvider } from '@clerk/clerk-react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider, createRouter } from '@tanstack/react-router'
import { Toaster } from 'sonner'
import './index.css'

// Import the generated route tree
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
  context: {
    queryClient,
  },
  defaultPreload: 'intent',
})

// Register the router instance for type safety
declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}

const clerkPubKey = import.meta.env.VITE_CLERK_PUBLISHABLE_KEY

if (!clerkPubKey || clerkPubKey.includes('YOUR_CLERK')) {
  console.warn(
    '⚠️ Clerk publishable key not set. Auth will be disabled in dev.'
  )
}

const root = document.getElementById('root')
if (!root) throw new Error('Root element not found')

createRoot(root).render(
  <StrictMode>
    <ClerkProvider
      publishableKey={clerkPubKey}
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
    </ClerkProvider>
  </StrictMode>
)
