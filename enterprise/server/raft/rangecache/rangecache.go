package rangecache

import (
	"sync"

	"github.com/buildbuddy-io/buildbuddy/enterprise/server/raft/constants"
	"github.com/buildbuddy-io/buildbuddy/server/util/log"
	"github.com/buildbuddy-io/buildbuddy/server/util/rangemap"
	"github.com/buildbuddy-io/buildbuddy/server/util/status"
	"github.com/golang/protobuf/proto"
	"github.com/hashicorp/serf/serf"

	rfpb "github.com/buildbuddy-io/buildbuddy/proto/raft"
)

type RangeCache struct {
	rangeMu sync.RWMutex

	// Keep a rangemap that maps from ranges to descriptors.
	rangeMap *rangemap.RangeMap
}

func New() *RangeCache {
	return &RangeCache{
		rangeMu:  sync.RWMutex{},
		rangeMap: rangemap.New(),
	}
}

func (rc *RangeCache) updateRange(rangeDescriptor *rfpb.RangeDescriptor) error {
	rc.rangeMu.Lock()
	defer rc.rangeMu.Unlock()

	left := rangeDescriptor.GetLeft()
	right := rangeDescriptor.GetRight()
	newDescriptor := proto.Clone(rangeDescriptor).(*rfpb.RangeDescriptor)

	r := rc.rangeMap.Get(left, right)
	if r == nil {
		// Easy add, there were no overlaps.
		_, err := rc.rangeMap.Add(left, right, newDescriptor)
		if err == nil {
			return nil
		}

		// If this range overlaps, we'll check it against the overlapping
		// ranges and possibly delete them or if they are newer, then we'll
		// ignore this update.
		for _, overlappingRange := range rc.rangeMap.GetOverlapping(left, right) {
			v, ok := overlappingRange.Val.(*rfpb.RangeDescriptor)
			if !ok {
				continue
			}
			if v.GetGeneration() >= newDescriptor.GetGeneration() {
				log.Debugf("Ignoring rangeDescriptor %+v, current generation: %d", newDescriptor, v.GetGeneration())
				return nil
			}
		}

		// If we got here, all overlapping ranges are older, so we're gonna
		// delete them and replace with the new one.
		for _, overlappingRange := range rc.rangeMap.GetOverlapping(left, right) {
			rc.rangeMap.Remove(overlappingRange.Left, overlappingRange.Right)
		}
		_, err = rc.rangeMap.Add(left, right, newDescriptor)
		return err
	} else {
		oldDescriptor, ok := r.Val.(*rfpb.RangeDescriptor)
		if !ok {
			return status.FailedPreconditionError("Val was not a rangeVal")
		}
		if newDescriptor.GetGeneration() > oldDescriptor.GetGeneration() {
			r.Val = newDescriptor
		}
	}
	return nil
}

// OnEvent is called when a node joins, leaves, or is updated.
func (rc *RangeCache) OnEvent(updateType serf.EventType, event serf.Event) {
	switch updateType {
	case serf.EventMemberJoin, serf.EventMemberUpdate:
		memberEvent, _ := event.(serf.MemberEvent)
		for _, member := range memberEvent.Members {
			if tagData, ok := member.Tags[constants.MetaRangeTag]; ok {
				rd := &rfpb.RangeDescriptor{}
				if err := proto.Unmarshal([]byte(tagData), rd); err != nil {
					log.Errorf("unparsable rangeset: %s", err)
					return
				}
				if err := rc.updateRange(rd); err != nil {
					log.Errorf("Error updating ranges: %s", err)
				}
			}
		}
	default:
		break
	}
}

func (rc *RangeCache) UpdateRange(rangeDescriptor *rfpb.RangeDescriptor) error {
	return rc.updateRange(rangeDescriptor)
}

func (rc *RangeCache) Get(key []byte) *rfpb.RangeDescriptor {
	rc.rangeMu.RLock()
	defer rc.rangeMu.RUnlock()

	val := rc.rangeMap.Lookup(key)
	if rv, ok := val.(*rfpb.RangeDescriptor); ok {
		return rv
	}
	return nil
}
