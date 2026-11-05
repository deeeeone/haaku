import { Button } from '@/components/ui'
import { UserPlus, UserCheck, Loader2 } from 'lucide-react'
import { useFollow, useUnfollow } from '@/api/hooks/useSocial'

interface FollowButtonProps {
  userId: string
  isFollowing?: boolean
  className?: string
  size?: 'sm' | 'lg' | 'icon' | undefined
}

export function FollowButton({ userId, isFollowing = false, className, size = 'sm' }: FollowButtonProps) {
  const follow = useFollow()
  const unfollow = useUnfollow()

  const isPending =
    follow.isPending || unfollow.isPending

  const handleClick = () => {
    if (isFollowing) {
      unfollow.mutate(userId)
    } else {
      follow.mutate(userId)
    }
  }

  return (
    <Button
      variant={isFollowing ? 'ghost' : 'primary'}
      size={size}
      onClick={handleClick}
      disabled={isPending}
      className={className}
    >
      {isPending ? (
        <Loader2 className="size-4 animate-spin" />
      ) : isFollowing ? (
        <UserCheck className="size-4" />
      ) : (
        <UserPlus className="size-4" />
      )}
      <span className="ml-1.5">
        {isFollowing ? 'Following' : 'Follow'}
      </span>
    </Button>
  )
}
