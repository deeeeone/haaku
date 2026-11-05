import { createFileRoute, Outlet } from '@tanstack/react-router'

export const Route = createFileRoute('/lists')({
  component: ListsLayout,
})

function ListsLayout() {
  return <Outlet />
}
