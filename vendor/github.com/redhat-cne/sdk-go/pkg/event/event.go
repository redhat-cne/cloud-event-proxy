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

package event

import (
	"fmt"
	"strings"

	"github.com/redhat-cne/sdk-go/pkg/types"
)

// Event represents the canonical representation of a Cloud Native Event.
// Event Json  payload is as follows,
//{
//	"id": "5ce55d17-9234-4fee-a589-d0f10cb32b8e",
//	"type": "event.synchronization-state-chang",
//	"time": "2021-02-05T17:31:00Z",
//	"data": {
//		"version": "v1.0",
//		"values": [{
//			"resource": "/cluster/node/ptp",
//			"data_type": "notification",
//			"value_type": "enumeration",
//			"value": "ACQUIRING-SYNC"
//			}, {
//			"resource": "/cluster/node/clock",
//			"data_type": "metric",
// 			"value_type": "decimal64.3",
//			"value": 100.3
//			}]
//		}
//}
//Event request model
type Event struct {
	// ID of the event; must be non-empty and unique within the scope of the producer.
	// +required
	ID string `json:"id" example:"789be75d-7ac3-472e-bbbc-6d62878aad4a"`
	// Type - The type of the occurrence which has happened.
	// +required
	Type string `json:"type" example:"event.synchronization-state-chang"`
	// DataContentType - the Data content type
	// +required
	DataContentType *string `json:"dataContentType" example:"application/json"`
	// Time - A Timestamp when the event happened.
	// +required
	Time *types.Timestamp `json:"time,omitempty" example:"2021-02-05T17:31:00Z"`
	// DataSchema - A link to the schema that the `Data` attribute adheres to.
	// +optional
	DataSchema *types.URI `json:"dataSchema,omitempty"`
	// +required
	Data *Data `json:"data,omitempty" `
}

// String returns a pretty-printed representation of the Event.
func (e Event) String() string {
	b := strings.Builder{}
	b.WriteString("  id: " + e.ID + "\n")
	b.WriteString("  type: " + e.Type + "\n")
	if e.Time != nil {
		b.WriteString("  time: " + e.Time.String() + "\n")
	}

	b.WriteString("  data: \n")
	b.WriteString("  version: " + e.Data.Version + "\n")
	b.WriteString("  values: \n")
	for _, v := range e.Data.Values {
		b.WriteString("  value type : " + string(v.ValueType) + "\n")
		b.WriteString("  data type : " + string(v.DataType) + "\n")
		b.WriteString("  value : " + fmt.Sprintf("%v", v.Value) + "\n")
		b.WriteString("  resource: " + v.GetResource() + "\n")
	}

	return b.String()
}

// Clone clones data
func (e Event) Clone() Event {
	out := Event{}
	out.SetData(*e.Data) //nolint:errcheck
	return out
}
