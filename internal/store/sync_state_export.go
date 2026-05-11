// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// SaveSyncState wraps the package-private saveSyncStateTx so callers in
// the cli package can record a sync cursor without owning a transaction
// themselves. The wrapper opens a short-lived transaction, delegates,
// then commits — useful for paginated sync loops that need to checkpoint
// progress between pages without forcing the caller to manage tx state.

package store

func (s *Store) SaveSyncState(propertyID, scope, lastDate string, rowCount int) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if err := s.saveSyncStateTx(tx, propertyID, scope, lastDate, rowCount); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
