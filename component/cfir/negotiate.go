package cfir

import "fmt"

type CapabilityRequirement struct {
	Standard   []StandardCapability
	Extensions []ExtensionID
}

func (r CapabilityRequirement) Validate() error {
	for _, capability := range r.Standard {
		if capability == CapabilityInvalid || capability >= standardCapabilityLimit {
			return fmt.Errorf("cfir: invalid required standard capability %d", capability)
		}
	}
	for _, extension := range r.Extensions {
		if _, err := ParseExtensionID(string(extension)); err != nil {
			return err
		}
	}
	return nil
}

func (r CapabilityRequirement) SatisfiedBy(offered CapabilitySet) bool {
	for _, capability := range r.Standard {
		if !offered.HasStandard(capability) {
			return false
		}
	}
	for _, extension := range r.Extensions {
		if !offered.HasExtension(extension) {
			return false
		}
	}
	return true
}

func (r CapabilityRequirement) MissingFrom(offered CapabilitySet) CapabilityRequirement {
	var missing CapabilityRequirement
	for _, capability := range r.Standard {
		if !offered.HasStandard(capability) {
			missing.Standard = append(missing.Standard, capability)
		}
	}
	for _, extension := range r.Extensions {
		if !offered.HasExtension(extension) {
			missing.Extensions = append(missing.Extensions, extension)
		}
	}
	return missing
}
