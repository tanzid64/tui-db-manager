package retention

import (
	"context"
	"sort"

	"github.com/tanzid64/tui-db-manager/internal/storage"
)

// Apply keeps the keepLast newest objects under jobPrefix and deletes the rest.
// keepLast <= 0 means "keep everything" and deletes nothing.
func Apply(ctx context.Context, be storage.Backend, jobPrefix string, keepLast int) ([]string, error) {
	if keepLast <= 0 {
		return nil, nil
	}
	objs, err := be.List(ctx, jobPrefix)
	if err != nil {
		return nil, err
	}
	if len(objs) <= keepLast {
		return nil, nil
	}
	// Newest first; tie-break on key so ordering is deterministic even when two
	// objects share a modification time.
	sort.Slice(objs, func(i, j int) bool {
		if objs[i].ModTime.Equal(objs[j].ModTime) {
			return objs[i].Key > objs[j].Key
		}
		return objs[i].ModTime.After(objs[j].ModTime)
	})
	var deleted []string
	for _, o := range objs[keepLast:] {
		if err := be.Delete(ctx, o.Key); err != nil {
			return deleted, err
		}
		deleted = append(deleted, o.Key)
	}
	return deleted, nil
}
