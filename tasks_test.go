package bichme

import "testing"

func TestTasks(t *testing.T) {
	all := map[Tasks]bool{
		KeepHistoryTask: true,
		ExecTask:        true,
		DownloadTask:    true,
		UploadTask:      false,
		CleanupTask:     false,
	}
	tt := KeepHistoryTask | ExecTask | DownloadTask
	cmpFlags(t, all, tt)
	if tt.Done() {
		t.Errorf("'%08b'.Done() should be false", tt)
	}

	tt.Set(CleanupTask)
	all[CleanupTask] = true
	cmpFlags(t, all, tt)

	tt.Unset(ExecTask)
	all[ExecTask] = false
	cmpFlags(t, all, tt)
	if tt.Done() {
		t.Errorf("'%08b'.Done() should be false", tt)
	}

	tt.Unset(KeepHistoryTask)
	tt.Unset(DownloadTask)
	tt.Unset(CleanupTask)
	if !tt.Done() {
		t.Errorf("'%08b'.Done() should be true", tt)
	}
}

func cmpFlags(t *testing.T, all map[Tasks]bool, input Tasks) {
	for flag, has := range all {
		if res := input.Has(flag); res != has {
			t.Errorf("'%08b'.Has(%08b) = %v, expected %v", input, flag, res, has)
		}
	}
}
