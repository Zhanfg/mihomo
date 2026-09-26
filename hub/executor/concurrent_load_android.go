//go:build android

package executor

// Large BoxProxy configurations can expose dozens of proxy/rule providers.
// Bound Android startup/reload fan-out so initialization stays parallel
// without creating desktop-sized CPU, socket and allocation spikes.
const concurrentCount = 8
