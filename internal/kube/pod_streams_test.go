package kube

import "testing"

func TestIsContainerShellMissingError(t *testing.T) {
	tests := []struct {
		name     string
		detail   string
		expected bool
	}{
		{
			name:     "binary missing",
			detail:   `OCI runtime exec failed: exec: "/bin/sh": stat /bin/sh: no such file or directory`,
			expected: true,
		},
		{
			name:     "executable not found",
			detail:   `exec: "bash": executable file not found in $PATH`,
			expected: true,
		},
		{
			name:     "permission denied",
			detail:   `permission denied`,
			expected: false,
		},
		{
			name:     "empty",
			detail:   ``,
			expected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if actual := isContainerShellMissingError(test.detail); actual != test.expected {
				t.Fatalf("expected %v, got %v", test.expected, actual)
			}
		})
	}
}
