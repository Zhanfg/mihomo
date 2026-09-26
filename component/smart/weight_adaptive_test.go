package smart

import (
	"math"
	"net/netip"
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


func TestModelCalibrationWeightTypeForIP(t *testing.T) {
	require.Equal(t, WeightTypeModelCalibrationTCP4, ModelCalibrationWeightTypeForIP(false, netip.MustParseAddr("1.1.1.1")))
	require.Equal(t, WeightTypeModelCalibrationTCP6, ModelCalibrationWeightTypeForIP(false, netip.MustParseAddr("2606:4700:4700::1111")))
	require.Equal(t, WeightTypeModelCalibrationUDP4, ModelCalibrationWeightTypeForIP(true, netip.MustParseAddr("8.8.8.8")))
	require.Equal(t, WeightTypeModelCalibrationUDP6, ModelCalibrationWeightTypeForIP(true, netip.MustParseAddr("2001:4860:4860::8888")))
	require.Equal(t, WeightTypeModelCalibrationTCP, ModelCalibrationWeightTypeForIP(false, netip.Addr{}))
}
