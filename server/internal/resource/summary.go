package resource

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// RawJSON is a modified version of json.RawMessage that strips whitespace in
// its UnmarshalJSON method.
type RawJSON []byte

func (a RawJSON) MarshalJSON() ([]byte, error) {
	if a == nil {
		return []byte("null"), nil
	}
	return a, nil
}

func (a *RawJSON) UnmarshalJSON(data []byte) error {
	if a == nil {
		return errors.New("RawJSON: UnmarshalJSON on nil pointer")
	}
	var b bytes.Buffer
	if err := json.Compact(&b, data); err != nil {
		return fmt.Errorf("failed to compact JSON data: %w", err)
	}
	*a = b.Bytes()
	return nil
}

type EventSummary struct {
	Type       EventType   `json:"type"`
	ResourceID string      `json:"resource_id"`
	Reason     EventReason `json:"reason,omitempty"`
	Diff       RawJSON     `json:"diff"`
}

func (e *Event) Summary() *EventSummary {
	diff, err := json.Marshal(e.Diff)
	if err != nil {
		diff = []byte("<invalid operation>")
	}
	return &EventSummary{
		Type:       e.Type,
		ResourceID: e.Resource.Identifier.String(),
		Reason:     e.Reason,
		Diff:       diff,
	}
}

type PlanSummary [][]*EventSummary

func (p Plan) Summary() PlanSummary {
	summary := make(PlanSummary, len(p))
	for i, phase := range p {
		phaseSummary := make([]*EventSummary, len(phase))
		for j, event := range phase {
			phaseSummary[j] = event.Summary()
		}
		slices.SortStableFunc(phaseSummary, func(a, b *EventSummary) int {
			return strings.Compare(a.ResourceID, b.ResourceID)
		})

		summary[i] = phaseSummary
	}
	return summary
}

func SummarizePlans(plans []Plan) []PlanSummary {
	summaries := make([]PlanSummary, len(plans))
	for i, p := range plans {
		summaries[i] = p.Summary()
	}
	return summaries
}
