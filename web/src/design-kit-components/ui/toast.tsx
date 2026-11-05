import { Toaster as SonnerToaster, toast as sonnerToast } from 'sonner'

export function Toaster() {
  return (
    <SonnerToaster
      theme="dark"
      position="bottom-right"
      toastOptions={{
        duration: 3500,
        style: {
          background: 'rgba(255,255,255,0.09)',
          backdropFilter: 'blur(20px) saturate(1.1)',
          WebkitBackdropFilter: 'blur(20px) saturate(1.1)',
          border: '1px solid rgba(255,255,255,0.14)',
          borderRadius: '16px',
          color: '#f8fbff',
          boxShadow: '0 22px 70px rgba(0,0,0,0.4)',
        },
        className: 'font-ui',
      }}
    />
  )
}

export { sonnerToast as toast }
