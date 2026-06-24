## 2025-02-20 - Naked Error Returns in CLI Mirror Logic
**Learning:** `cli/internal/registry/mirror.go` contained numerous naked error returns, obscuring the source of failures during local git mirror operations (e.g., failing to differentiate between a `clone` error, a directory creation error, or a file read/write error during `copyTree`).
**Action:** Wrapped all naked errors with `fmt.Errorf("...: %w", err)` to provide clear context for debugging mirror synchronization issues without breaking the original error chain.
