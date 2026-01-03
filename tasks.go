package bichme

// Tasks is a bit mask that can hold all the things a job should do.
type Tasks int8

const (
	KeepHistoryTask Tasks = 1 << iota
	ExecTask
	DownloadTask
	UploadTask
	CleanupTask
)

// Has reports whether flag is set in t.
func (t Tasks) Has(flag Tasks) bool { return t&flag != 0 }

// Set given flag into t.
func (t *Tasks) Set(flag Tasks) { *t |= flag }

// Unset given flag from t.
func (t *Tasks) Unset(flag Tasks) { *t &^= flag }

// Done reports wheter all flags from t are unset.
func (t *Tasks) Done() bool { return *t == 0 }
