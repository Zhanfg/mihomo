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


func TestAdaptModelPredictionReliabilityFallsOnPersistentError(t *testing.T) {
	_, _, err1 := AdaptModelPredictionWithReliability(1.0, 1.8, 1.0, 0, 64)
	weight2, _, err2 := AdaptModelPredictionWithReliability(1.0, 1.8, 1.0, err1, 128)
	require.Greater(t, err2, 0.1)
	require.Greater(t, weight2, 1.0)
	require.Less(t, weight2, 1.8)
}

func TestShouldInvokeModelUsesSparseStableCadence(t *testing.T) {
	input := &ModelInput{Success: 260}
	require.True(t, ShouldInvokeModel(input, 0.03)) // 260 % 13 == 0

	input.Success = 261
	require.False(t, ShouldInvokeModel(input, 0.03))

	input.ConnectionFailed = true
	require.True(t, ShouldInvokeModel(input, 0.03))
}

func TestShouldInvokeModelRechecksUnreliableModelMoreOften(t *testing.T) {
	input := &ModelInput{Success: 66}
	require.True(t, ShouldInvokeModel(input, 0.25)) // 66 % 3 == 0
	require.False(t, ShouldInvokeModel(input, 0.05))
}

func TestModelErrorWeightType(t *testing.T) {
	require.Equal(t, WeightTypeModelErrorTCP, ModelErrorWeightType(false))
	require.Equal(t, WeightTypeModelErrorUDP, ModelErrorWeightType(true))
}
