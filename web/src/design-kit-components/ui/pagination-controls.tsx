import { ChevronLeft, ChevronRight } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Button } from './button'
import { Input } from './input'
import { Number } from './number'

interface PaginationControlsProps {
  currentPage: number
  totalPages: number
  onPageChange: (page: number) => void
  className?: string
  paginationLabel?: string
  previousPageLabel?: string
  previousLabel?: string
  pageLabel?: string
  currentPageLabel?: string
  ofLabel?: string
  nextLabel?: string
  nextPageLabel?: string
}

export function PaginationControls({
  currentPage,
  totalPages,
  onPageChange,
  className,
  paginationLabel = 'Pagination',
  previousPageLabel = 'Previous page',
  previousLabel = 'Previous',
  pageLabel = 'Page',
  currentPageLabel = 'Current page number',
  ofLabel = 'of',
  nextLabel = 'Next',
  nextPageLabel = 'Next page',
}: PaginationControlsProps) {
  const [inputPage, setInputPage] = useState(currentPage.toString())

  useEffect(() => {
    queueMicrotask(() => {
      setInputPage(currentPage.toString())
    })
  }, [currentPage])

  const handleInputChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setInputPage(e.target.value)
  }

  const handleInputBlur = () => {
    const page = parseInt(inputPage, 10)
    if (page >= 1 && page <= totalPages) {
      if (page !== currentPage) {
        onPageChange(page)
      }
    } else {
      setInputPage(currentPage.toString())
    }
  }

  const handleInputKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter') {
      e.currentTarget.blur()
    }
  }

  if (totalPages <= 1) return null

  return (
    <nav
      className={`flex items-center justify-center gap-2 ${className || ''}`}
      aria-label={paginationLabel}
    >
      <Button
        variant="ghost"
        size="sm"
        onClick={() => onPageChange(currentPage - 1)}
        disabled={currentPage === 1}
        aria-label={previousPageLabel}
      >
        <ChevronLeft className="h-4 w-4" aria-hidden="true" />
        {previousLabel}
      </Button>

      <div className="flex items-center gap-2">
        <span className="text-sm text-sf-ink-hint">{pageLabel}</span>
        <Input
          size="sm"
          className="text-center"
          style={{ width: `${inputPage.length + 4}ch` }}
          value={inputPage}
          onChange={handleInputChange}
          onBlur={handleInputBlur}
          onKeyDown={handleInputKeyDown}
          aria-label={currentPageLabel}
        />
        <span className="text-sm text-sf-ink-hint whitespace-nowrap">
          {ofLabel} <Number value={totalPages} />
        </span>
      </div>

      <Button
        variant="ghost"
        size="sm"
        onClick={() => onPageChange(currentPage + 1)}
        disabled={currentPage === totalPages}
        aria-label={nextPageLabel}
      >
        {nextLabel}
        <ChevronRight className="h-4 w-4" aria-hidden="true" />
      </Button>
    </nav>
  )
}
