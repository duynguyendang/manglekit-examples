// testdata/destructive_methods.go — method-form fixture: the v1.0 extractor
// regex missed `func (r *T) PurgeX()` (dogfood finding 2026-09-10). Only ONE
// signal here: destructive_call via methods.
package destructive

type repo struct{}

func (r *repo) PurgeArchive(dir string) error { _ = dir; return nil }

func (r repo) DropAllCaches() {}
