package metrics

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_isOffsetInRange(t *testing.T) {
	tests := []struct {
		name               string
		ptpOffset          int64
		maxOffsetThreshold int64
		expected           bool
	}{
		{
			name:               "in-range positive offset",
			ptpOffset:          50,
			maxOffsetThreshold: 100,
			expected:           true,
		},
		{
			name:               "in-range negative offset",
			ptpOffset:          -50,
			maxOffsetThreshold: 100,
			expected:           true,
		},
		{
			name:               "out-of-range positive offset",
			ptpOffset:          150,
			maxOffsetThreshold: 100,
			expected:           false,
		},
		{
			name:               "out-of-range negative offset",
			ptpOffset:          -150,
			maxOffsetThreshold: 100,
			expected:           false,
		},
		{
			name:               "exact positive boundary (non-inclusive)",
			ptpOffset:          100,
			maxOffsetThreshold: 100,
			expected:           false,
		},
		{
			name:               "exact negative boundary (non-inclusive)",
			ptpOffset:          -100,
			maxOffsetThreshold: 100,
			expected:           false,
		},
		{
			name:               "zero offset",
			ptpOffset:          0,
			maxOffsetThreshold: 100,
			expected:           true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := isOffsetInRange(tt.ptpOffset, tt.maxOffsetThreshold)
			assert.Equal(t, tt.expected, actual)
		})
	}
}
