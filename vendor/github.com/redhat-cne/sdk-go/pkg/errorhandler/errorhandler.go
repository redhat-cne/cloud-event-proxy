// Copyright 2020 The Cloud Native Events Authors
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

package errorhandler

import "fmt"

// ErrorHandler  ... custom error handler interface
type ErrorHandler interface {
	Error() string
}

// ReceiverError receiver general error
type ReceiverError struct {
	Name string
	Desc string
}

// Error receiver general error string
func (r ReceiverError) Error() string {
	return fmt.Sprintf("receiver %s error %s", r.Name, r.Desc)
}

// SenderNotFoundError sender not found custom error
type SenderNotFoundError struct {
	Name string
	Desc string
}

// Error sender not found error string
func (s SenderNotFoundError) Error() string {
	return fmt.Sprintf("sender %s not found", s.Name)
}

// HTTPConnectionError custom http connection error
type HTTPConnectionError struct {
	Desc string
}

// Error HTTPConnectionError connection error string
func (a HTTPConnectionError) Error() string {
	return fmt.Sprintf("http connection error %s", a.Desc)
}

// CloudEventsClientError custom cloud events client error
type CloudEventsClientError struct {
	Desc string
}

// Error cloud events clients error string
func (c CloudEventsClientError) Error() string {
	return fmt.Sprintf(" cloud events client error %s", c.Desc)
}
