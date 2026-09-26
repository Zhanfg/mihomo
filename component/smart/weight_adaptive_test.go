package smart

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdaptModelPredictionLearnsBoundedResidual(t *testing.T) {
	weight, calibration := AdaptModelPrediction(1.0, 1.30, 1.0, 32)
	require.Greater(t, calibration, 1.0)
	require.LessOrEqual(t, calibration, 1.35)
	require.Greater(t, weight, 1.0)
	require.Less(t, weight, 1.30)
}

func TestAdaptModelPredictionPreservesBounds(t *testing.T) {
	_, high := AdaptModelPrediction(1.0, 10.0, 1.35, 1000)
	_, low := AdaptModelPrediction(1.0, 0.01, 0.65, 1000)
	require.LessOrEqual(t, high, 1.35)
	require.GreaterOrEqual(t, low, 0.65)
}

func TestAdaptModelPredictionInvalidModelDoesNotExplode(t *testing.T) {
	weight, calibration := AdaptModelPrediction(math.NaN(), 1.0, 0, 10)
	require.True(t, math.IsNaN(weight))
	require.Equal(t, 1.0, calibration)
}

func TestModelCalibrationWeightType(t *testing.T) {
	require.Equal(t, WeightTypeModelCalibrationTCP, ModelCalibrationWeightType(false))
	require.Equal(t, WeightTypeModelCalibrationUDP, ModelCalibrationWeightType(true))
}

func TestModelCalibrationWeightTypeForInputSeparatesFamilyAndScene(t *testing.T) {
	v4Web := &ModelInput{IsTCP: true, DestIP: "1.1.1.1", Latency: 80}
	v6Web := &ModelInput{IsTCP: true, DestIP: "2606:4700:4700::1111", Latency: 80}
	v6Stream := &ModelInput{
		IsTCP:              true,
		DestIP:             "2606:4700:4700::1111",
		Latency:            80,
		DownloadTotal:      64,
		MaxdownloadRate:    8192,
		ConnectionDuration: 12,
	}

	k4 := ModelCalibrationWeightTypeForInput(v4Web)
	k6 := ModelCalibrationWeightTypeForInput(v6Web)
	ks := ModelCalibrationWeightTypeForInput(v6Stream)
	require.NotEqual(t, k4, k6)
	require.NotEqual(t, k6, ks)
	require.Contains(t, k4, ":v4:")
	require.Contains(t, k6, ":v6:")
}
