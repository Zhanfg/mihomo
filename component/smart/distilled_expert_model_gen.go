package smart

// Code-generated-style distilled artifact for Smart expert-v8.
// The table is the exact fold of expertCoefficients × expertSceneGates for each
// {scene, transport} bucket. Keep it small enough to stay hot in cache.
const DistilledExpertVersion = "expert-v8-1"

var distilledExpertCoefficients = [8][ExpertFeatureDimension]float64{
	// web/tcp
	{0.04, 0.3122, 0.17, 0.2148, 0.1676, 0.069, 0.0264},
	// web/udp
	{0.04, 0.31697431, 0.17318744, 0.22125595, 0.16795433, 0.05523311, 0.02539486},
	// interactive/tcp
	{0.04, 0.2961, 0.1899, 0.265, 0.15, 0.034, 0.025},
	// interactive/udp
	{0.04, 0.29582143, 0.19319643, 0.27258929, 0.14834821, 0.02549107, 0.02455357},
	// streaming/tcp
	{0.04, 0.275, 0.1242, 0.1364, 0.1528, 0.238, 0.0336},
	// streaming/udp
	{0.04, 0.29459459, 0.13039501, 0.14453222, 0.16035343, 0.1991684, 0.03095634},
	// transfer/tcp
	{0.04, 0.244, 0.1115, 0.118, 0.141, 0.3075, 0.038},
	// transfer/udp
	{0.04, 0.26361644, 0.11687671, 0.12421918, 0.1489863, 0.27082192, 0.03547945},
}

// Per-bucket production calibration is deliberately a separate one-number
// layer. smart-distill can rewrite these scalars from collected production
// samples without changing the expert architecture or shipping raw telemetry.
var distilledExpertCalibration = [8]float64{
	1, 1, 1, 1, 1, 1, 1, 1,
}
