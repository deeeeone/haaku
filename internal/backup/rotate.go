package backup

import "context"

// Rotate prunes complete backups past the retention count and sweeps incomplete
// folders that were abandoned. It runs under the subsystem lock. A scheduled
// run calls it after a backup; Backup calls rotateLocked under its own lock.
//
// Complete backups are kept newest-first: the newest Keep of the nightly,
// manual and before-restore kinds, and the two newest before-upgrade copies
// separately. An incomplete folder is never a valid backup: it is not counted
// toward the retention count, is never deleted in place of a complete one, and
// is swept only once its newest object is older than IncompleteGrace. An
// unreadable folder is kept, never rotated and never swept.
func (s *Service) Rotate(ctx context.Context) ([]string, error) {
	unlock, err := s.opts.Locker.Lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock.Unlock()
	return s.rotateLocked(ctx)
}

func (s *Service) rotateLocked(ctx context.Context) ([]string, error) {
	folders, err := s.listFolders(ctx)
	if err != nil {
		return nil, err
	}
	sortNewestFirst(folders)

	var deleted []string
	keptOther, keptUpgrade := 0, 0
	for _, f := range folders {
		status, _ := s.inspect(ctx, f)
		switch status {
		case StatusComplete:
			if f.kind == KindBeforeUpgrade {
				if keptUpgrade < beforeUpgradeKeep {
					keptUpgrade++
					continue
				}
			} else if keptOther < s.opts.Keep {
				keptOther++
				continue
			}
			if err := s.deleteFolder(ctx, f); err != nil {
				return deleted, err
			}
			deleted = append(deleted, f.id)
		case StatusIncomplete:
			// An incomplete folder is only swept once it is clearly
			// abandoned; a run that is still writing holds the lock.
			if s.opts.Now().Sub(f.newestModTime()) > s.opts.IncompleteGrace {
				if err := s.deleteFolder(ctx, f); err != nil {
					return deleted, err
				}
				deleted = append(deleted, f.id)
			}
		default:
			// StatusUnreadable: kept, never rotated, never swept.
		}
	}
	return deleted, nil
}
