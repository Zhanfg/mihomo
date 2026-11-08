package lightgbm

import "fmt"

// ValidateModelFeatureSchema rejects models which declare an incompatible
// transform order. A larger LightGBM tree ensemble is fine if it retains
// Smart V11's 30-feature input contract; a different feature order is not.
func ValidateModelFeatureSchema(path string) error {
	transforms, err := LoadTransformsFromModel(path)
	if err != nil {
		// Legacy upstream LightGBM models do not always carry transforms.
		// The core already treats their missing metadata as default ordering.
		return nil
	}
	if !transforms.TransformsEnabled {
		return nil
	}
	if err := transforms.ValidateTransforms(MaxFeatureSize); err != nil {
		return fmt.Errorf("invalid Smart feature transforms: %w", err)
	}
	if !transforms.IsCompatibleWith(getDefaultFeatureOrder()) {
		return fmt.Errorf("Smart LightGBM feature order is incompatible with current core")
	}
	return nil
}
