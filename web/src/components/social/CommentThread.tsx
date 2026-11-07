import {
  ChevronDown,
  Edit3,
  EyeOff,
  Flag,
  Loader2,
  MessageSquare,
  ThumbsUp,
  Trash2,
} from 'lucide-react'
import { useState } from 'react'
import {
  useAddReaction,
  useCommentReplies,
  useComments,
  useCreateComment,
  useCreateReply,
  useDeleteComment,
  useEditComment,
  useLikeComment,
  useRemoveReaction,
  useReportComment,
  useUnlikeComment,
} from '@/api/hooks/useSocial'
import type { Comment } from '@/api/types'
import { useAuth } from '@/auth/context'
import { ReactionPicker } from '@/components/social/ReactionPicker'
import { SpoilerOverlay } from '@/components/social/SpoilerOverlay'
import { Button, PaginationControls } from '@/components/ui'
import { cn } from '@/lib/utils'

function timeAgo(dateStr: string): string {
  const date = new Date(dateStr)
  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const diffSec = Math.floor(diffMs / 1000)
  const diffMin = Math.floor(diffSec / 60)
  const diffHr = Math.floor(diffMin / 60)
  const diffDay = Math.floor(diffHr / 24)

  if (diffSec < 60) return 'just now'
  if (diffMin < 60) return `${diffMin}m ago`
  if (diffHr < 24) return `${diffHr}h ago`
  if (diffDay < 7) return `${diffDay}d ago`
  return date.toLocaleDateString()
}

interface SingleCommentProps {
  comment: Comment
  depth?: number
  onReply: () => void
}

function SingleComment({ comment, depth = 0, onReply }: SingleCommentProps) {
  const { isSignedIn } = useAuth()
  const [showSpoiler, setShowSpoiler] = useState(comment.spoiler)
  const [editing, setEditing] = useState(false)
  const [editText, setEditText] = useState(comment.comment)
  const [editSpoiler, setEditSpoiler] = useState(comment.spoiler)
  const [showReplies, setShowReplies] = useState(false)
  const [replyText, setReplyText] = useState('')
  const [replySpoiler, setReplySpoiler] = useState(false)
  const [replying, setReplying] = useState(false)
  const [activeReactions, setActiveReactions] = useState<string[]>([])
  const [reactionCounts, setReactionCounts] = useState<Record<string, number>>(
    {}
  )

  const editMutation = useEditComment()
  const deleteMutation = useDeleteComment()
  const likeMutation = useLikeComment()
  const unlikeMutation = useUnlikeComment()
  const addReaction = useAddReaction()
  const removeReaction = useRemoveReaction()
  const reportMutation = useReportComment()
  const createReply = useCreateReply(comment.id)

  const { data: repliesData } = useCommentReplies(comment.id, { limit: 10 })
  const replies = repliesData?.data ?? []

  const isPending =
    editMutation.isPending ||
    deleteMutation.isPending ||
    likeMutation.isPending ||
    unlikeMutation.isPending ||
    createReply.isPending

  const isOwner = false // Would need current user slug from Clerk

  const handleLike = () => {
    likeMutation.mutate(comment.id)
  }

  const handleReaction = (reaction: string) => {
    if (activeReactions.includes(reaction)) {
      removeReaction.mutate({ commentId: comment.id, reaction })
      setActiveReactions((prev) => prev.filter((r) => r !== reaction))
      setReactionCounts((prev) => ({
        ...prev,
        [reaction]: Math.max(0, (prev[reaction] ?? 1) - 1),
      }))
    } else {
      addReaction.mutate({ commentId: comment.id, reaction })
      setActiveReactions((prev) => [...prev, reaction])
      setReactionCounts((prev) => ({
        ...prev,
        [reaction]: (prev[reaction] ?? 0) + 1,
      }))
    }
  }

  const handleEdit = () => {
    editMutation.mutate(
      { id: comment.id, comment: editText, spoiler: editSpoiler },
      { onSuccess: () => setEditing(false) }
    )
  }

  const handleDelete = () => {
    if (window.confirm('Delete this comment?')) {
      deleteMutation.mutate(comment.id)
    }
  }

  const handleReplySubmit = () => {
    if (!replyText.trim()) return
    createReply.mutate(
      { comment: replyText, spoiler: replySpoiler },
      {
        onSuccess: () => {
          setReplyText('')
          setReplySpoiler(false)
          setReplying(false)
          setShowReplies(true)
        },
      }
    )
  }

  const bodyLines = comment.comment.split('\n')

  return (
    <div
      className={cn('group', depth > 0 && 'ml-8 border-l border-white/5 pl-4')}
    >
      <div
        className={cn(
          'relative rounded-[--sf-radius] p-3',
          isPending && 'opacity-50'
        )}
      >
        {/* Spoiler overlay */}
        <SpoilerOverlay
          hidden={showSpoiler}
          onToggle={() => setShowSpoiler(false)}
        />

        {/* Header */}
        <div className="mb-1 flex items-center gap-2">
          <div className="flex h-6 w-6 items-center justify-center rounded-full bg-sf-accent/20 text-xs font-bold text-sf-accent-2">
            {comment.user?.username?.[0]?.toUpperCase() ?? '?'}
          </div>
          <span className="text-sm font-medium text-sf-ink">
            {comment.user?.name ?? comment.user?.username ?? 'User'}
          </span>
          <span className="text-xs text-sf-ink-muted">
            {timeAgo(comment.created_at)}
          </span>
          {comment.updated_at && comment.updated_at !== comment.created_at && (
            <span className="text-xs text-sf-ink-muted">(edited)</span>
          )}
          {comment.spoiler && (
            <span className="rounded-full bg-yellow-400/10 px-1.5 py-0.5 text-[10px] font-medium text-yellow-400">
              Spoiler
            </span>
          )}
        </div>

        {/* Body */}
        {editing ? (
          <div className="mt-2 space-y-2">
            <textarea
              value={editText}
              onChange={(e) => setEditText(e.target.value)}
              className="w-full rounded-[--sf-radius] border border-white/10 bg-white/5 p-2 text-sm text-sf-ink placeholder:text-sf-ink-muted focus:border-sf-accent-2 focus:outline-none"
              rows={3}
            />
            <div className="flex items-center gap-2">
              <label className="flex items-center gap-1.5 text-xs text-sf-ink-muted cursor-pointer">
                <input
                  type="checkbox"
                  checked={editSpoiler}
                  onChange={(e) => setEditSpoiler(e.target.checked)}
                  className="size-3 rounded"
                />
                Spoiler
              </label>
              <div className="flex gap-1.5">
                <Button
                  size="sm"
                  variant="primary"
                  onClick={handleEdit}
                  disabled={editMutation.isPending}
                >
                  {editMutation.isPending ? (
                    <Loader2 className="size-3 animate-spin" />
                  ) : (
                    'Save'
                  )}
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => setEditing(false)}
                >
                  Cancel
                </Button>
              </div>
            </div>
          </div>
        ) : (
          <div className="text-sm text-sf-ink">
            {bodyLines.map((line, i) => (
              <p key={i} className="leading-relaxed">
                {line || '\u00A0'}
              </p>
            ))}
          </div>
        )}

        {/* Actions bar */}
        {!editing && (
          <div className="mt-2 flex flex-wrap items-center gap-1">
            <ReactionPicker
              onReaction={handleReaction}
              activeReactions={activeReactions}
              reactionCounts={reactionCounts}
            />

            <button
              type="button"
              onClick={handleLike}
              className="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs text-sf-ink-muted transition-colors hover:bg-white/10 hover:text-sf-ink"
            >
              <ThumbsUp className="size-3" />
              {comment.likes > 0 && comment.likes}
            </button>

            {isSignedIn && (
              <button
                type="button"
                onClick={() => {
                  onReply()
                  setReplying(!replying)
                }}
                className="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs text-sf-ink-muted transition-colors hover:bg-white/10 hover:text-sf-ink"
              >
                <MessageSquare className="size-3" />
                Reply
              </button>
            )}

            {isOwner && (
              <>
                <button
                  type="button"
                  onClick={() => {
                    setEditing(true)
                    setEditText(comment.comment)
                  }}
                  className="rounded-full p-1 text-sf-ink-muted transition-colors hover:bg-white/10 hover:text-sf-ink"
                >
                  <Edit3 className="size-3" />
                </button>
                <button
                  type="button"
                  onClick={handleDelete}
                  className="rounded-full p-1 text-sf-ink-muted transition-colors hover:bg-white/10 hover:text-red-400"
                >
                  <Trash2 className="size-3" />
                </button>
              </>
            )}

            <button
              type="button"
              onClick={() => reportMutation.mutate(comment.id)}
              className="ml-auto rounded-full p-1 text-sf-ink-muted opacity-0 transition-opacity hover:bg-white/10 hover:text-sf-ink group-hover:opacity-100"
              title="Report"
            >
              <Flag className="size-3" />
            </button>
          </div>
        )}

        {/* Reply form */}
        {replying && isSignedIn && (
          <div className="mt-3 ml-8 space-y-2">
            <textarea
              value={replyText}
              onChange={(e) => setReplyText(e.target.value)}
              placeholder="Write a reply..."
              className="w-full rounded-[--sf-radius] border border-white/10 bg-white/5 p-2 text-sm text-sf-ink placeholder:text-sf-ink-muted focus:border-sf-accent-2 focus:outline-none"
              rows={2}
            />
            <div className="flex items-center gap-2">
              <label className="flex items-center gap-1.5 text-xs text-sf-ink-muted cursor-pointer">
                <input
                  type="checkbox"
                  checked={replySpoiler}
                  onChange={(e) => setReplySpoiler(e.target.checked)}
                  className="size-3 rounded"
                />
                Spoiler
              </label>
              <Button
                size="sm"
                variant="primary"
                onClick={handleReplySubmit}
                disabled={createReply.isPending}
              >
                {createReply.isPending ? (
                  <Loader2 className="size-3 animate-spin" />
                ) : (
                  'Reply'
                )}
              </Button>
              <Button
                size="sm"
                variant="ghost"
                onClick={() => setReplying(false)}
              >
                Cancel
              </Button>
            </div>
          </div>
        )}
      </div>

      {/* Replies toggle */}
      {replies.length > 0 && (
        <div className="ml-8 mt-1">
          <button
            type="button"
            onClick={() => setShowReplies(!showReplies)}
            className="flex items-center gap-1 text-xs text-sf-accent-2 hover:underline"
          >
            <ChevronDown
              className={cn(
                'size-3 transition-transform',
                showReplies && 'rotate-180'
              )}
            />
            {showReplies
              ? 'Hide'
              : `Show ${replies.length} ${replies.length === 1 ? 'reply' : 'replies'}`}
          </button>

          {showReplies && (
            <div className="mt-2 space-y-1">
              {replies.map((reply) => (
                <SingleComment
                  key={reply.id}
                  comment={reply}
                  depth={depth + 1}
                  onReply={() => {
                    setReplying(true)
                  }}
                />
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

// ─── Main CommentThread ────────────────────────────────────────────────────

interface CommentThreadProps {
  mediaType: string // 'movie' | 'show' | 'episode' | 'season'
  mediaId: string
  className?: string
}

export function CommentThread({
  mediaType,
  mediaId,
  className,
}: CommentThreadProps) {
  const { isSignedIn } = useAuth()
  const [page, setPage] = useState(1)
  const [commentText, setCommentText] = useState('')
  const [spoiler, setSpoiler] = useState(false)

  const { data, isLoading } = useComments({
    mediaType,
    mediaId,
    page,
    limit: 10,
  })

  const createComment = useCreateComment()

  const comments = data?.data ?? []
  const totalPages = data?.total_pages ?? 1

  const handleSubmit = () => {
    if (!commentText.trim()) return
    createComment.mutate(
      {
        comment: commentText,
        spoiler,
        review: false,
        item: { type: mediaType, id: Number(mediaId) },
      },
      {
        onSuccess: () => {
          setCommentText('')
          setSpoiler(false)
        },
      }
    )
  }

  return (
    <div className={cn('space-y-4', className)}>
      {/* Comment input */}
      {isSignedIn && (
        <div className="space-y-2">
          <textarea
            value={commentText}
            onChange={(e) => setCommentText(e.target.value)}
            placeholder="Add a comment..."
            className="w-full rounded-[--sf-radius] border border-white/10 bg-white/5 p-3 text-sm text-sf-ink placeholder:text-sf-ink-muted focus:border-sf-accent-2 focus:outline-none"
            rows={3}
          />
          <div className="flex items-center justify-between">
            <label className="flex items-center gap-1.5 text-sm text-sf-ink-muted cursor-pointer">
              <input
                type="checkbox"
                checked={spoiler}
                onChange={(e) => setSpoiler(e.target.checked)}
                className="size-3.5 rounded"
              />
              <EyeOff className="size-3.5" />
              Contains spoilers
            </label>
            <Button
              variant="primary"
              size="sm"
              onClick={handleSubmit}
              disabled={createComment.isPending || !commentText.trim()}
            >
              {createComment.isPending ? (
                <Loader2 className="size-4 animate-spin" />
              ) : (
                'Post Comment'
              )}
            </Button>
          </div>
        </div>
      )}

      {/* Comments list */}
      {isLoading ? (
        <div className="space-y-3">
          {Array.from({ length: 3 }).map((_, i) => (
            <div
              key={i}
              className="animate-pulse space-y-2 rounded-[--sf-radius] p-3"
            >
              <div className="flex items-center gap-2">
                <div className="h-6 w-6 rounded-full bg-white/10" />
                <div className="h-4 w-24 rounded bg-white/10" />
              </div>
              <div className="h-4 w-3/4 rounded bg-white/5" />
              <div className="h-4 w-1/2 rounded bg-white/5" />
            </div>
          ))}
        </div>
      ) : comments.length === 0 ? (
        <div className="rounded-[--sf-radius] py-8 text-center">
          <MessageSquare className="mx-auto size-8 text-sf-ink-muted" />
          <p className="mt-2 text-sm text-sf-ink-muted">
            No comments yet. Be the first!
          </p>
        </div>
      ) : (
        <div className="space-y-1">
          {comments.map((comment) => (
            <SingleComment
              key={comment.id}
              comment={comment}
              onReply={() => {}}
            />
          ))}

          {totalPages > 1 && (
            <div className="flex justify-center pt-4">
              <PaginationControls
                currentPage={page}
                totalPages={totalPages}
                onPageChange={setPage}
              />
            </div>
          )}
        </div>
      )}
    </div>
  )
}
