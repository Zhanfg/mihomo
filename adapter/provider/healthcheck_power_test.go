package provider

import "testing"

func TestHealthCheckParallelismPolicy(t *testing.T) {
	if got := healthCheckParallelism(false); got != 10 {
		t.Fatalf("desktop parallelism=%d want=10", got)
	}
	if got := healthCheckParallelism(true); got != 2 {
		t.Fatalf("Android parallelism=%d want=2", got)
	}
	if cap(androidHealthProbeSem) != androidGlobalHealthProbeLimit {
		t.Fatalf("global Android probe cap=%d want=%d", cap(androidHealthProbeSem), androidGlobalHealthProbeLimit)
	}
}
