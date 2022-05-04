// Copyright 2022 The Cloud Native Events Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package common

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetIntEnv(t *testing.T) {
	testCases := []struct {
		keyToLookup    string
		testValue      string
		expectedOutput int
	}{
		{
			keyToLookup:    "tc1",
			testValue:      "1337",
			expectedOutput: 1337,
		},
		{
			keyToLookup:    "tc2",
			testValue:      "0",
			expectedOutput: 0,
		},
		{
			keyToLookup:    "tc3",
			testValue:      "", // empty string means 0
			expectedOutput: 0,
		},
	}

	for _, tc := range testCases {
		os.Setenv(tc.keyToLookup, tc.testValue)
		assert.Equal(t, tc.expectedOutput, GetIntEnv(tc.keyToLookup))
		os.Unsetenv(tc.keyToLookup)
	}
}

func TestGetFloatEnv(t *testing.T) {
	testCases := []struct {
		keyToLookup    string
		testValue      string
		expectedOutput float64
	}{
		{
			keyToLookup:    "tc1",
			testValue:      "1337.37",
			expectedOutput: 1337.37,
		},
		{
			keyToLookup:    "tc2",
			testValue:      "0.00",
			expectedOutput: 0.00,
		},
		{
			keyToLookup:    "tc3",
			testValue:      "", // empty string means 0
			expectedOutput: 0,
		},
	}

	for _, tc := range testCases {
		os.Setenv(tc.keyToLookup, tc.testValue)
		assert.Equal(t, tc.expectedOutput, GetFloatEnv(tc.keyToLookup))
		os.Unsetenv(tc.keyToLookup)
	}
}

func TestGetBoolEnv(t *testing.T) {
	testCases := []struct {
		keyToLookup    string
		testValue      string
		expectedOutput bool
	}{
		{
			keyToLookup:    "tc1",
			testValue:      "true",
			expectedOutput: true,
		},
		{
			keyToLookup:    "tc2",
			testValue:      "false",
			expectedOutput: false,
		},
		{
			keyToLookup:    "tc3",
			testValue:      "", // empty string means false
			expectedOutput: false,
		},
	}

	for _, tc := range testCases {
		os.Setenv(tc.keyToLookup, tc.testValue)
		assert.Equal(t, tc.expectedOutput, GetBoolEnv(tc.keyToLookup))
		os.Unsetenv(tc.keyToLookup)
	}
}
