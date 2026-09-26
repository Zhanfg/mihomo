//go:build !android && (386 || amd64 || arm64 || arm64be)

package executor

import "math"

const concurrentCount = math.MaxInt
