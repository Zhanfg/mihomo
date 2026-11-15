package statistic

import "testing"

func uploadSignaled(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func TestP1UploadSignalWakeOnlyOnNewUploads(t *testing.T) {
	m := &Manager{}
	var tracker TrackerInfo
	m.noteUpload(&tracker.LastUpload, 12)
	first := m.UploadSignal()
	if uploadSignaled(first) { t.Fatal("woke on a write preceding arming") }
	if next := m.UploadSignal(); next != first { t.Fatal("changed an armed wake channel") }
	m.noteUpload(&tracker.LastUpload, 0)
	if uploadSignaled(first) { t.Fatal("zero-byte upload woke the sweep") }
	m.noteUpload(&tracker.LastUpload, 1)
	if !uploadSignaled(first) { t.Fatal("positive upload failed to wake the sweep") }
	next := m.UploadSignal()
	if uploadSignaled(next) { t.Fatal("old upload leaked into the next wake") }
}

func TestP1ReplyPendingRequiresUnansweredWrite(t *testing.T) {
	var info TrackerInfo
	if info.ReplyPending() { t.Fatal("idle socket appears stalled") }
	info.LastUpload.Store(10)
	if !info.ReplyPending() { t.Fatal("unanswered write not detected") }
	info.LastDownload.Store(11)
	if info.ReplyPending() { t.Fatal("download falsely left reply pending") }
}
