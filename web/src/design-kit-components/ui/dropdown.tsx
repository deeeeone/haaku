import * as DropdownMenuPrimitive from '@radix-ui/react-dropdown-menu'
import type * as React from 'react'

import { cn } from '../../lib/utils'

export type DropdownItem = {
  label: string
  icon?: React.ReactNode
  shortcut?: string
  danger?: boolean
  onSelect?: () => void
}

export type DropdownProps = {
  trigger: React.ReactNode
  items: DropdownItem[]
  align?: 'start' | 'center' | 'end'
  className?: string
}

export function Dropdown({
  trigger,
  items,
  align = 'end',
  className,
}: DropdownProps) {
  return (
    <DropdownMenuPrimitive.Root>
      <DropdownMenuPrimitive.Trigger asChild>
        {trigger}
      </DropdownMenuPrimitive.Trigger>
      <DropdownMenuPrimitive.Portal>
        <DropdownMenuPrimitive.Content
          align={align}
          sideOffset={10}
          className={cn(
            'glass z-50 min-w-52 rounded-2xl border-sf-line p-1.5 shadow-(--sf-shadow)',
            'data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:zoom-in-95',
            'data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-95',
            className
          )}
        >
          {items.map((item) => (
            <DropdownMenuPrimitive.Item
              key={item.label}
              onSelect={item.onSelect}
              className={cn(
                'flex cursor-pointer items-center gap-2.5 rounded-xl px-3 py-2 text-sm font-medium outline-none select-none',
                'data-[highlighted]:bg-sf-panel-2 data-[highlighted]:text-sf-ink',
                item.danger
                  ? 'text-[#ff7a82] data-[highlighted]:bg-sf-brand-red/10'
                  : 'text-sf-ink-muted'
              )}
            >
              {item.icon && (
                <span className="flex size-4 items-center justify-center opacity-70">
                  {item.icon}
                </span>
              )}
              <span className="flex-1">{item.label}</span>
              {item.shortcut && (
                <span className="text-[10px] font-bold text-sf-ink-hint">
                  {item.shortcut}
                </span>
              )}
            </DropdownMenuPrimitive.Item>
          ))}
        </DropdownMenuPrimitive.Content>
      </DropdownMenuPrimitive.Portal>
    </DropdownMenuPrimitive.Root>
  )
}
Dropdown.displayName = 'Dropdown'
