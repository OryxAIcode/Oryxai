package cli

import "os"

// removeIfExists deletes p if present; returns nil if the file was
// missing so callers can `_ = removeIfExists(p)` without ceremony.
func removeIfExists(p string) error {
	err := os.Remove(p)
	if err != nil && os.IsNotExist(err) {
		return nil
	}
	return err
}
