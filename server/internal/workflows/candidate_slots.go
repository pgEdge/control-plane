package workflows

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/cschleiden/go-workflows/workflow"

	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

const (
	// candidateSlotsTimeout is how long a switchover or failover waits for a
	// replica to hold persistent copies of the primary's Spock slots.
	candidateSlotsTimeout = 2 * time.Minute
	// candidateSlotsPollInterval is how often the slots are checked.
	candidateSlotsPollInterval = 5 * time.Second
)

// missingSlotCopies returns the names of the primary's usable, persistent
// Spock slots that the replica doesn't hold as usable, persistent slots.
//
// Spock copies each slot to a replica as a temporary slot, and persists the
// copy once the primary's slot has moved past the replica's position.
// Promotion drops temporary slots, so promoting a replica that lacks a
// persistent copy leaves the peer subscriptions that read from that slot
// without one, and they stop replicating. An invalidated copy can't serve
// them either. An invalidated slot on the primary is skipped, because no copy
// of it could serve a subscriber.
func missingSlotCopies(primary, replica []postgres.SpockSlot) []string {
	ready := make(map[string]bool, len(replica))
	for _, slot := range replica {
		if !slot.Temporary && slot.Usable {
			ready[slot.Name] = true
		}
	}
	var missing []string
	for _, slot := range primary {
		if !slot.Temporary && slot.Usable && !ready[slot.Name] {
			missing = append(missing, slot.Name)
		}
	}
	return missing
}

// candidateOrder returns the instances to consider for promotion, in order of
// preference: the requested candidate only, if there is one, or otherwise the
// preferred instance followed by the node's other non-leader instances.
func candidateOrder(
	instances []*activities.InstanceHost,
	leaderInstanceID, requested, preferred string,
) []*activities.InstanceHost {
	var first, rest []*activities.InstanceHost
	for _, inst := range instances {
		if inst == nil || inst.InstanceID == leaderInstanceID {
			continue
		}
		switch {
		case requested != "":
			if inst.InstanceID == requested {
				return []*activities.InstanceHost{inst}
			}
		case inst.InstanceID == preferred:
			first = append(first, inst)
		default:
			rest = append(rest, inst)
		}
	}
	return append(first, rest...)
}

type chooseCandidateInput struct {
	DatabaseID       string
	NodeName         string
	Instances        []*activities.InstanceHost
	LeaderInstanceID string
	LeaderHostID     string
	// Requested is the candidate the caller asked for, if any.
	Requested string
}

// chooseCandidate picks the instance to promote in a switchover or failover.
// It only returns a candidate that holds persistent copies of all of the
// primary's Spock slots, waiting up to candidateSlotsTimeout for one. If the
// primary's slots can't be read, for example because the primary is down, it
// can't tell which copies are needed and returns the first candidate.
func (w *Workflows) chooseCandidate(ctx workflow.Context, logger *slog.Logger, in *chooseCandidateInput) (string, error) {
	if in.Requested == in.LeaderInstanceID && in.Requested != "" {
		return in.Requested, nil
	}

	var preferred string
	if in.Requested == "" {
		selOut, err := w.Activities.ExecuteSelectCandidate(ctx, &activities.SelectCandidateInput{
			DatabaseID:      in.DatabaseID,
			NodeName:        in.NodeName,
			ExcludeInstance: in.LeaderInstanceID,
			Instances:       in.Instances,
		}).Get(ctx)
		if err != nil {
			return "", fmt.Errorf("candidate selection failed: %w", err)
		}
		if selOut == nil || selOut.CandidateInstanceID == "" {
			return "", fmt.Errorf("no eligible candidate found")
		}
		preferred = selOut.CandidateInstanceID
	}

	candidates := candidateOrder(in.Instances, in.LeaderInstanceID, in.Requested, preferred)
	if len(candidates) == 0 {
		// The requested instance isn't one of this node's instances. Leave it
		// to the switchover or failover to report that.
		return in.Requested, nil
	}

	deadline := workflow.Now(ctx).Add(candidateSlotsTimeout)
	for {
		primaryOut, err := w.Activities.ExecuteGetSpockSlots(ctx, in.LeaderHostID, &activities.GetSpockSlotsInput{
			DatabaseID: in.DatabaseID,
			InstanceID: in.LeaderInstanceID,
		}).Get(ctx)
		if err != nil {
			logger.Warn("failed to read the primary's spock slots; promoting without checking the candidate's slot copies",
				"error", err, "candidate_instance", candidates[0].InstanceID)
			return candidates[0].InstanceID, nil
		}

		var missing []string
		for _, candidate := range candidates {
			replicaOut, err := w.Activities.ExecuteGetSpockSlots(ctx, candidate.HostID, &activities.GetSpockSlotsInput{
				DatabaseID: in.DatabaseID,
				InstanceID: candidate.InstanceID,
			}).Get(ctx)
			if err != nil {
				logger.Warn("failed to read candidate's spock slots", "error", err, "candidate_instance", candidate.InstanceID)
				missing = []string{"(unreadable)"}
				continue
			}
			missing = missingSlotCopies(primaryOut.Slots, replicaOut.Slots)
			if len(missing) == 0 {
				return candidate.InstanceID, nil
			}
			logger.Info("candidate doesn't have persistent copies of the primary's spock slots yet",
				"candidate_instance", candidate.InstanceID, "missing_slots", strings.Join(missing, ","))
		}

		if !workflow.Now(ctx).Before(deadline) {
			if in.Requested != "" {
				return "", fmt.Errorf("candidate instance %s has no persistent copy of spock replication slot(s) %s after %s; promoting it would stop peer nodes from replicating from node %s, try again later",
					in.Requested, strings.Join(missing, ", "), candidateSlotsTimeout, in.NodeName)
			}
			return "", fmt.Errorf("no replica of node %s has persistent copies of all spock replication slots after %s; promoting one would stop peer nodes from replicating from it, try again later",
				in.NodeName, candidateSlotsTimeout)
		}
		if err := workflow.Sleep(ctx, candidateSlotsPollInterval); err != nil {
			return "", err
		}
	}
}
