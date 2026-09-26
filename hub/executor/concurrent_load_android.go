//go:build android

package executor

// Android should not initialize every proxy/rule provider at once. Large
// BoxProxy configurations can contain dozens of remote providers; an unbounded
// fan-out creates avoidable CPU, socket and allocation spikes during boot and
// reload. Eight keeps startup parallel without behaving like a desktop host.
const concurrentCount = 8
