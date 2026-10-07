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


func TestTrainingSampleRatePrioritizesInformativeRows(t *testing.T) {
	stable := &ModelInput{Success: 300}
	require.Equal(t, 0.125, TrainingSampleRate(stable, 0.03, 1.0))

	normal := &ModelInput{Success: 80}
	require.Equal(t, 0.25, TrainingSampleRate(normal, 0.10, 1.0))

	failed := &ModelInput{Success: 300, ConnectionFailed: true}
	require.Equal(t, 1.0, TrainingSampleRate(failed, 0.03, 1.0))

	lossy := &ModelInput{Success: 300, LossRate: 0.02}
	require.Equal(t, 1.0, TrainingSampleRate(lossy, 0.03, 1.0))
}


func BenchmarkShouldInvokeModelStable(b *testing.B) {
	input := &ModelInput{Success: 512}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		input.Success = 512 + int64(i%13)
		_ = ShouldInvokeModel(input, 0.03)
	}
}

func BenchmarkAdaptModelPredictionWithReliability(b *testing.B) {
	b.ReportAllocs()
	calibration, modelError := 1.0, 0.05
	for i := 0; i < b.N; i++ {
		_, calibration, modelError = AdaptModelPredictionWithReliability(
			1.05, 1.02, calibration, modelError, 256+int64(i&63),
		)
	}
}


func TestTrainingSampleRateHonorsConfiguredCeiling(t *testing.T) {
	failed := &ModelInput{Success: 40, ConnectionFailed: true}
	require.Equal(t, 0.2, TrainingSampleRate(failed, 0.30, 0.2))

	stable := &ModelInput{Success: 300}
	require.Equal(t, 0.025, TrainingSampleRate(stable, 0.03, 0.2))
}


func TestShouldInvokeTeacherUsesStudentConfidence(t *testing.T) {
	stable := &ModelInput{Success: 290}
	require.True(t, ShouldInvokeTeacher(stable, 0.03, 0.10, 0.04, 300)) // 290 % 29 == 0

	stable.Success = 291
	require.False(t, ShouldInvokeTeacher(stable, 0.03, 0.10, 0.04, 300))

	uncertain := &ModelInput{Success: 66}
	require.True(t, ShouldInvokeTeacher(uncertain, 0.03, 0.70, 0.05, 80)) // 66 % 3 == 0

	uncertain.Success = 67
	require.False(t, ShouldInvokeTeacher(uncertain, 0.03, 0.70, 0.05, 80))
}

func TestShouldInvokeTeacherEscalatesFailureAndColdStudent(t *testing.T) {
	failed := &ModelInput{Success: 300, ConnectionFailed: true}
	require.True(t, ShouldInvokeTeacher(failed, 0.01, 0.05, 0.02, 500))

	lossy := &ModelInput{Success: 300, LossRate: 0.02}
	require.True(t, ShouldInvokeTeacher(lossy, 0.01, 0.05, 0.02, 500))

	cold := &ModelInput{Success: 12}
	require.True(t, ShouldInvokeTeacher(cold, 0.01, 1, 0, 3))
}

func BenchmarkShouldInvokeTeacherStable(b *testing.B) {
	input := &ModelInput{Success: 512}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		input.Success = 512 + int64(i%29)
		_ = ShouldInvokeTeacher(input, 0.03, 0.10, 0.04, 300)
	}
}
