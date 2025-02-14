package gossip_test

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/buildbuddy-io/buildbuddy/server/gossip"
	"github.com/buildbuddy-io/buildbuddy/server/testutil/app"
	"github.com/hashicorp/serf/serf"
	"github.com/stretchr/testify/require"
)

type testBroker struct {
	onEvent func(eventType serf.EventType, event serf.Event)
}

func (b *testBroker) OnEvent(eventType serf.EventType, event serf.Event) {
	if b.onEvent != nil {
		b.onEvent(eventType, event)
	}
}

func localAddr(t *testing.T) string {
	return fmt.Sprintf("127.0.0.1:%d", app.FreePort(t))
}

func newGossipManager(t *testing.T, addr string, seeds []string, broker gossip.Listener) *gossip.GossipManager {
	node, err := gossip.NewGossipManager(addr, seeds)
	require.Nil(t, err)
	require.NotNil(t, node)
	node.AddListener(broker)
	return node
}

func TestDiscovery(t *testing.T) {
	node1Addr := localAddr(t)
	node2Addr := localAddr(t)

	node1 := newGossipManager(t, node1Addr, []string{node2Addr}, &testBroker{})
	defer node1.Shutdown()

	sawNode1 := make(chan struct{})
	doneChecking := false
	eventCB := func(eventType serf.EventType, event serf.Event) {
		if memberEvent, ok := event.(serf.MemberEvent); ok {
			for _, member := range memberEvent.Members {
				if !doneChecking {
					if fmt.Sprintf("%s:%d", member.Addr, member.Port) == node1Addr {
						doneChecking = true
						close(sawNode1)
					}
				}
			}
		}
	}

	node2 := newGossipManager(t, localAddr(t), []string{node1Addr}, &testBroker{onEvent: eventCB})
	defer node2.Shutdown()

	select {
	case <-time.After(10 * time.Second):
		t.Fatalf("Timed out waiting for nodes to discover each other via gossip")
	case <-sawNode1:
		break
	}
}

func TestSendTag(t *testing.T) {
	node1Addr := localAddr(t)
	broker1 := &testBroker{}
	node1 := newGossipManager(t, node1Addr, nil /*=seeds*/, broker1)
	defer node1.Shutdown()

	sawTag := make(chan struct{})
	eventCB := func(eventType serf.EventType, event serf.Event) {
		if memberEvent, ok := event.(serf.MemberEvent); ok {
			for _, member := range memberEvent.Members {
				tagVal, ok := member.Tags["testTagName"]
				if ok && tagVal == "testTagValue" {
					close(sawTag)
				}
			}
		}
	}

	node2 := newGossipManager(t, localAddr(t), []string{node1Addr}, &testBroker{onEvent: eventCB})
	defer node2.Shutdown()

	err := node1.SetTag("testTagName", "testTagValue")
	require.Nil(t, err)

	select {
	case <-time.After(10 * time.Second):
		t.Fatalf("Timed out waiting for tags to be received")
	case <-sawTag:
		break
	}
}

func removeDuplicates(dups []string) []string {
	m := make(map[string]struct{})
	out := make([]string, 0)
	for _, s := range dups {
		if _, ok := m[s]; !ok {
			out = append(out, s)
			m[s] = struct{}{}
		}
	}
	return out
}

func TestUserQuery(t *testing.T) {
	data := make(map[string][]string, 0)

	addrs := make([]string, 0)
	for i := 0; i < 5; i++ {
		addr := localAddr(t)
		addrs = append(addrs, addr)
		for j := 0; j < 5; j++ {
			letterByte := string(byte('a' + i*5 + j))
			data[addr] = append(data[addr], letterByte)
		}
	}

	nodes := make([]*gossip.GossipManager, 0)
	for i, nodeAddr := range addrs {
		nodeAddr := nodeAddr
		b := &testBroker{
			onEvent: func(eventType serf.EventType, event serf.Event) {
				if query, ok := event.(*serf.Query); ok {
					if query.Name == "letters" {
						err := query.Respond([]byte(strings.Join(data[nodeAddr], ",")))
						require.Nil(t, err)
					}
				}
			},
		}
		n := newGossipManager(t, nodeAddr, addrs[:i], b)
		nodes = append(nodes, n)
		defer n.Shutdown()
	}

	mu := sync.Mutex{}
	receivedLetters := make([]string, 0)
	n := newGossipManager(t, localAddr(t), addrs, &testBroker{})
	defer n.Shutdown()
	go func() {
		rsp, err := n.Query("letters", nil, nil)
		require.Nil(t, err)
		for nodeResponse := range rsp.ResponseCh() {
			mu.Lock()
			letters := strings.Split(string(nodeResponse.Payload), ",")
			receivedLetters = append(receivedLetters, letters...)
			mu.Unlock()
		}
	}()

	seenItAll := make(chan struct{})
	go func() {
		for {
			mu.Lock()
			letters := removeDuplicates(receivedLetters)
			mu.Unlock()

			sort.Strings(letters)
			if strings.Join(letters, "") == "abcdefghijklmnopqrstuvwxy" {
				close(seenItAll)
				break
			}
		}
	}()

	select {
	case <-time.After(3 * time.Second):
		t.Fatalf("Timed out waiting for tags to be received: %+v", receivedLetters)
	case <-seenItAll:
		break
	}
}

func TestUserEvents(t *testing.T) {
	addrs := make([]string, 0)
	for i := 0; i < 5; i++ {
		addrs = append(addrs, localAddr(t))
	}
	// map of nodeAddr => received bits
	mu := sync.Mutex{}
	gotData := make(map[string][]string, 0)
	nodes := make([]*gossip.GossipManager, 0)
	for i, nodeAddr := range addrs {
		nodeAddr := nodeAddr
		b := &testBroker{
			onEvent: func(eventType serf.EventType, event serf.Event) {
				if userEvent, ok := event.(serf.UserEvent); ok {
					mu.Lock()
					gotData[nodeAddr] = append(gotData[nodeAddr], string(userEvent.Payload))
					mu.Unlock()
				}
			},
		}
		n := newGossipManager(t, nodeAddr, addrs[:i], b)
		nodes = append(nodes, n)
		defer n.Shutdown()
	}

	for i, n := range nodes {
		for j := 0; j < 5; j++ {
			letterByte := byte('a' + i*5 + j)
			if err := n.SendUserEvent("letter", []byte{letterByte}, false); err != nil {
				t.Fatalf("error sending user event: %s", err)
			}
		}
	}

	seenItAll := make(chan struct{})
	go func() {
		done := make(map[string]struct{})
		for {
			for _, addr := range addrs {
				mu.Lock()
				sort.Strings(gotData[addr])
				nodeGot := strings.Join(gotData[addr], "")
				mu.Unlock()
				if nodeGot == "abcdefghijklmnopqrstuvwxy" {
					done[addr] = struct{}{}
				}
			}
			if len(done) == len(addrs) {
				close(seenItAll)
				break
			}
		}
	}()

	select {
	case <-time.After(3 * time.Second):
		t.Fatalf("Timed out waiting for tags to be received: %+v", gotData)
	case <-seenItAll:
		break
	}
}
