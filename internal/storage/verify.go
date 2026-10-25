package storage

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Verify checks that a backend can really store backups: it writes a small
// file, reads its metadata back, and deletes it. This catches wrong
// credentials, a missing bucket, or a read-only folder before the first real
// backup runs.
func Verify(ctx context.Context, be Backend) error {
	if err := be.Test(ctx); err != nil {
		return err
	}
	key := fmt.Sprintf(".dbk-write-test-%d", time.Now().UnixNano())
	if err := be.Put(ctx, key, strings.NewReader("dbk write test\n"), 15); err != nil {
		return fmt.Errorf("write test file: %w", err)
	}
	if _, err := be.Stat(ctx, key); err != nil {
		_ = be.Delete(ctx, key)
		return fmt.Errorf("read back test file: %w", err)
	}
	if err := be.Delete(ctx, key); err != nil {
		return fmt.Errorf("delete test file: %w", err)
	}
	return nil
}
