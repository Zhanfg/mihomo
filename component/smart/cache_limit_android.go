//go:build android

package smart

// Smart history remains persisted in bbolt. This limit only bounds the hot
// in-memory LRU working set so a large provider/rule configuration cannot make
// Android RAM usage scale like a desktop process.
func platformMaxTargetsLimit() int { return 2048 }
