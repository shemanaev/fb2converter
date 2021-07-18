package utils

import (
	"strings"
)

// AppendIfMissing well append string to slice only if it is not there already.
func AppendIfMissing(slice []string, str string) []string {
	for _, s := range slice {
		if s == str {
			return slice
		}
	}
	return append(slice, str)
}

// AppendIfMissingIgnoreCase well append string to slice only if it is not there already.
func AppendIfMissingIgnoreCase(slice []string, str string) []string {
	for _, s := range slice {
		if strings.EqualFold(s, str) {
			return slice
		}
	}
	return append(slice, str)
}

// IsOneOf checks if string is present in slice of strings.
func IsOneOf(name string, names []string) bool {
	for _, n := range names {
		if name == n {
			return true
		}
	}
	return false
}

// IsOneOfIgnoreCase checks if string is present in slice of strings. Comparison is case insensitive.
func IsOneOfIgnoreCase(name string, names []string) bool {
	for _, n := range names {
		if strings.EqualFold(name, n) {
			return true
		}
	}
	return false
}

// EqualStringSlices checks if 2 slices of strings are equal.
func EqualStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}
	return true
}
