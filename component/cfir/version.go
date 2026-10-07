package cfir

import "fmt"

// Version is the compatibility version of the CoreFlow IR contract.
//
// Minor versions are append-only: a runtime that implements 1.4 can consume an
// adapter requiring 1.0 through 1.4. A major version change is reserved for a
// semantic break that cannot be represented as an additive capability or
// extension.
type Version struct {
	Major uint16
	Minor uint16
}

var CurrentVersion = Version{Major: 1, Minor: 0}

func (v Version) String() string {
	return fmt.Sprintf("%d.%d", v.Major, v.Minor)
}

func (v Version) Supports(required Version) bool {
	return v.Major == required.Major && v.Minor >= required.Minor
}

func (v Version) Valid() bool {
	return v.Major != 0
}
