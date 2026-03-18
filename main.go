package main

import (
	"fmt"
	"time"

	"example/raft/raft"
)

// ---- helpers ----

func makeCluster(n int) ([]*raft.Raft, []chan raft.LogEntry) {
	applyChs := make([]chan raft.LogEntry, n)
	nodes := make([]*raft.Raft, n)

	for i := 0; i < n; i++ {
		applyChs[i] = make(chan raft.LogEntry, 100)
		nodes[i] = raft.NewRaft(i, []string{}, applyChs[i])
	}

	for i := 0; i < n; i++ {
		nodes[i].PeerRaft = nodes
	}

	return nodes, applyChs
}

func killAll(nodes []*raft.Raft) {
	for _, n := range nodes {
		n.Kill()
	}
}

func waitForLeader(nodes []*raft.Raft, timeout time.Duration) *raft.Raft {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, n := range nodes {
			if !n.IsDead() && n.IsLeader() {
				return n
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil
}

func countLeaders(nodes []*raft.Raft) (int, *raft.Raft) {
	count := 0
	var leader *raft.Raft
	for _, n := range nodes {
		if !n.IsDead() && n.IsLeader() {
			count++
			leader = n
		}
	}
	return count, leader
}

func waitForApplied(ch chan raft.LogEntry, count int, timeout time.Duration) []raft.LogEntry {
	var entries []raft.LogEntry
	deadline := time.After(timeout)
	for len(entries) < count {
		select {
		case e := <-ch:
			entries = append(entries, e)
		case <-deadline:
			return entries
		}
	}
	return entries
}

var passed, failed int

func pass(name string) {
	fmt.Printf("✅ PASS: %s\n", name)
	passed++
}

func fail(name, reason string) {
	fmt.Printf("❌ FAIL: %s — %s\n", name, reason)
	failed++
}

// ---- tests ----

func testLeaderElected() {
	name := "TestLeaderElected"
	nodes, _ := makeCluster(5)
	defer killAll(nodes)

	leader := waitForLeader(nodes, 5*time.Second)
	if leader == nil {
		fail(name, "no leader elected within 5s")
		return
	}

	count, _ := countLeaders(nodes)
	if count != 1 {
		fail(name, fmt.Sprintf("expected 1 leader, got %d", count))
		return
	}

	fmt.Printf("   Leader: Node %d term=%d\n", leader.ID(), leader.Term())
	pass(name)
}

func testOnlyOneLeader() {
	name := "TestOnlyOneLeader"
	nodes, _ := makeCluster(5)
	defer killAll(nodes)

	if waitForLeader(nodes, 5*time.Second) == nil {
		fail(name, "no initial leader elected")
		return
	}

	for i := 0; i < 10; i++ {
		time.Sleep(200 * time.Millisecond)
		count, _ := countLeaders(nodes)
		if count > 1 {
			fail(name, fmt.Sprintf("found %d leaders simultaneously", count))
			return
		}
	}

	pass(name)
}

func testReElectionAfterLeaderDeath() {
	name := "TestReElectionAfterLeaderDeath"
	nodes, _ := makeCluster(5)
	defer killAll(nodes)

	leader := waitForLeader(nodes, 5*time.Second)
	if leader == nil {
		fail(name, "no initial leader elected")
		return
	}

	oldID := leader.ID()
	oldTerm := leader.Term()
	fmt.Printf("   Killing leader Node %d term=%d\n", oldID, oldTerm)
	nodes[oldID].Kill()

	newLeader := waitForLeader(nodes, 5*time.Second)
	if newLeader == nil {
		fail(name, "no new leader elected after killing old leader")
		return
	}

	if newLeader.ID() == oldID {
		fail(name, "new leader is the same dead node")
		return
	}

	if newLeader.Term() <= oldTerm {
		fail(name, fmt.Sprintf("expected term > %d, got %d", oldTerm, newLeader.Term()))
		return
	}

	fmt.Printf("   New leader: Node %d term=%d\n", newLeader.ID(), newLeader.Term())
	pass(name)
}

func testNoLeaderWithoutQuorum() {
	name := "TestNoLeaderWithoutQuorum"
	nodes, _ := makeCluster(5)
	defer killAll(nodes)

	if waitForLeader(nodes, 5*time.Second) == nil {
		fail(name, "no initial leader elected")
		return
	}

	// kill 3/5 — quorum needs 3, only 2 remain
	nodes[0].Kill()
	nodes[1].Kill()
	nodes[2].Kill()
	fmt.Println("   Killed nodes 0, 1, 2 — only 2 alive")

	time.Sleep(3 * time.Second)

	count, _ := countLeaders(nodes)
	if count > 0 {
		fail(name, fmt.Sprintf("expected no leader, got %d", count))
		return
	}

	pass(name)
}

func testSubmitCommand() {
	name := "TestSubmitCommand"
	nodes, applyChs := makeCluster(5)
	defer killAll(nodes)

	leader := waitForLeader(nodes, 5*time.Second)
	if leader == nil {
		fail(name, "no leader elected")
		return
	}

	ok := leader.SubmitCommand("set x=1")
	if !ok {
		fail(name, "SubmitCommand returned false on leader")
		return
	}

	entries := waitForApplied(applyChs[leader.ID()], 1, 3*time.Second)
	if len(entries) == 0 {
		fail(name, "command never applied to applyCh")
		return
	}

	if entries[0].Command != "set x=1" {
		fail(name, fmt.Sprintf("expected 'set x=1', got '%s'", entries[0].Command))
		return
	}

	fmt.Printf("   Applied: %s\n", entries[0].Command)
	pass(name)
}

func testSubmitMultipleCommands() {
	name := "TestSubmitMultipleCommands"
	nodes, applyChs := makeCluster(5)
	defer killAll(nodes)

	leader := waitForLeader(nodes, 5*time.Second)
	if leader == nil {
		fail(name, "no leader elected")
		return
	}

	commands := []string{"cmd1", "cmd2", "cmd3", "cmd4", "cmd5"}
	for _, cmd := range commands {
		if !leader.SubmitCommand(cmd) {
			fail(name, fmt.Sprintf("SubmitCommand failed for %s", cmd))
			return
		}
	}

	entries := waitForApplied(applyChs[leader.ID()], len(commands), 5*time.Second)
	if len(entries) != len(commands) {
		fail(name, fmt.Sprintf("expected %d entries, got %d", len(commands), len(entries)))
		return
	}

	for i, e := range entries {
		if e.Command != commands[i] {
			fail(name, fmt.Sprintf("entry %d: expected %s got %s", i, commands[i], e.Command))
			return
		}
	}

	pass(name)
}

func testSubmitOnNonLeaderFails() {
	name := "TestSubmitOnNonLeaderFails"
	nodes, _ := makeCluster(5)
	defer killAll(nodes)

	if waitForLeader(nodes, 5*time.Second) == nil {
		fail(name, "no leader elected")
		return
	}

	var follower *raft.Raft
	for _, n := range nodes {
		if !n.IsDead() && !n.IsLeader() {
			follower = n
			break
		}
	}

	if follower == nil {
		fail(name, "could not find a follower")
		return
	}

	if follower.SubmitCommand("should fail") {
		fail(name, "SubmitCommand returned true on follower")
		return
	}

	pass(name)
}

func testTermsOnlyIncrease() {
	name := "TestTermsOnlyIncrease"
	nodes, _ := makeCluster(5)
	defer killAll(nodes)

	if waitForLeader(nodes, 5*time.Second) == nil {
		fail(name, "no initial leader")
		return
	}

	terms := make([]int, len(nodes))
	for i, n := range nodes {
		terms[i] = n.Term()
	}

	for round := 0; round < 3; round++ {
		_, leader := countLeaders(nodes)
		if leader != nil {
			fmt.Printf("   Round %d: killing leader Node %d\n", round+1, leader.ID())
			nodes[leader.ID()].Kill()
		}
		time.Sleep(2 * time.Second)

		for i, n := range nodes {
			if !n.IsDead() {
				newTerm := n.Term()
				if newTerm < terms[i] {
					fail(name, fmt.Sprintf("node %d term decreased %d → %d", i, terms[i], newTerm))
					return
				}
				terms[i] = newTerm
			}
		}
	}

	pass(name)
}

func testCommandReplicatedToFollowers() {
	name := "TestCommandReplicatedToFollowers"
	nodes, applyChs := makeCluster(5)
	defer killAll(nodes)

	leader := waitForLeader(nodes, 5*time.Second)
	if leader == nil {
		fail(name, "no leader elected")
		return
	}

	time.Sleep(500 * time.Millisecond)
	leader.SubmitCommand("replicate me")

	applied := 0
	for i, n := range nodes {
		if n.IsDead() {
			continue
		}
		entries := waitForApplied(applyChs[i], 1, 3*time.Second)
		if len(entries) > 0 && entries[0].Command == "replicate me" {
			applied++
			fmt.Printf("   Node %d applied the command\n", n.ID())
		}
	}

	if applied < len(nodes)/2+1 {
		fail(name, fmt.Sprintf("only %d/%d nodes applied, need majority", applied, len(nodes)))
		return
	}

	fmt.Printf("   Replicated to %d/%d nodes\n", applied, len(nodes))
	pass(name)
}

// ---- main ----

func main() {
	tests := []struct {
		name string
		fn   func()
	}{
		{"TestLeaderElected", testLeaderElected},
		{"TestOnlyOneLeader", testOnlyOneLeader},
		{"TestReElectionAfterLeaderDeath", testReElectionAfterLeaderDeath},
		{"TestNoLeaderWithoutQuorum", testNoLeaderWithoutQuorum},
		{"TestSubmitCommand", testSubmitCommand},
		{"TestSubmitMultipleCommands", testSubmitMultipleCommands},
		{"TestSubmitOnNonLeaderFails", testSubmitOnNonLeaderFails},
		{"TestTermsOnlyIncrease", testTermsOnlyIncrease},
		{"TestCommandReplicatedToFollowers", testCommandReplicatedToFollowers},
	}

	passed := 0
	failed := 0

	fmt.Println("=== Running Raft Tests ===")
	fmt.Println()

	for _, test := range tests {
		fmt.Printf("--- %s\n", test.name)
		test.fn()
		fmt.Println()
	}

	fmt.Printf("=== Results: %d passed, %d failed ===\n", passed, failed)
}

// func main() {
// 	rand.Seed(time.Now().UnixNano())

// 	const (
// 		numNodes      = 5
// 		killRounds    = 6
// 		roundInterval = 3 * time.Second
// 	)

// 	applyCh := make(chan raft.LogEntry)

// 	// Create Raft nodes
// 	rafts := make([]*raft.Raft, numNodes)
// 	for i := 0; i < numNodes; i++ {
// 		rafts[i] = raft.NewRaft(i, nil, applyCh)
// 	}

// 	// Wire peers
// 	for i := 0; i < numNodes; i++ {
// 		rafts[i].PeerRaft = rafts
// 	}

// 	fmt.Println("🚀 Raft cluster started with", numNodes, "nodes")

// 	time.Sleep(3 * time.Second)

// 	for round := 1; round <= killRounds; round++ {
// 		fmt.Printf("\n🔥 CHAOS ROUND %d 🔥\n", round)

// 		printClusterState(rafts)

// 		victim := chooseRandomAlive(rafts)
// 		if victim == nil {
// 			fmt.Println("❌ No alive nodes left")
// 			break
// 		}

// 		fmt.Printf("💀 Killing node %d (role=%s)\n", victim.ID(), victim.Role())
// 		victim.Kill()

// 		time.Sleep(roundInterval)
// 	}

// 	fmt.Println("\n🧪 Chaos test complete")
// 	select {}
// }

// func chooseRandomAlive(rafts []*raft.Raft) *raft.Raft {
// 	alive := []*raft.Raft{}
// 	for _, rf := range rafts {
// 		if !rf.IsDead() {
// 			alive = append(alive, rf)
// 		}
// 	}

// 	if len(alive) == 0 {
// 		return nil
// 	}

// 	return alive[rand.Intn(len(alive))]
// }

// func printClusterState(rafts []*raft.Raft) {
// 	fmt.Println("Cluster state:")
// 	for _, rf := range rafts {
// 		status := "alive"
// 		if rf.IsDead() {
// 			status = "dead"
// 		}

// 		fmt.Printf(
// 			"  Node %d | role=%s | term=%d | %s\n",
// 			rf.ID(),
// 			rf.Role(),
// 			rf.Term(),
// 			status,
// 		)
// 	}
// }

// func main() {
// 	const n = 3
// 	applyCh := make(chan raft.LogEntry)

// 	peers := []string{
// 		"node1",
// 		"node2",
// 		"node3",
// 	}

// 	r1 := raft.NewRaft(0, peers, applyCh)
// 	r2 := raft.NewRaft(1, peers, applyCh)
// 	r3 := raft.NewRaft(2, peers, applyCh)

// 	peerRaft := []*raft.Raft{
// 		r1,
// 		r2,
// 		r3,
// 	}
// 	// peerRaft[rand.Intn(3)].MakeLeader()

// 	r1.PeerRaft = peerRaft
// 	r2.PeerRaft = peerRaft
// 	r3.PeerRaft = peerRaft

// 	fmt.Println("🚀 Raft cluster started")
// 	// Let elections happen
// 	time.Sleep(3 * time.Second)

// 	leader := findLeader(peerRaft)
// 	if leader == nil {
// 		fmt.Println("❌ No leader elected")
// 		return
// 	}

// 	fmt.Println("👑 Initial leader:", leader.ID())

// 	// Kill the leader
// 	fmt.Println("😴 Putting leader to sleep...")
// 	leader.Kill()

// 	// Wait for re-election
// 	time.Sleep(3 * time.Second)

// 	newLeader := findLeader(peerRaft)
// 	if newLeader == nil {
// 		fmt.Println("❌ No new leader elected")
// 		return
// 	}

// 	fmt.Println("👑 New leader elected:", newLeader.ID())

// 	fmt.Println("✅ Election robustness test complete")
// 	return
// }

// func findLeader(rafts []*raft.Raft) *raft.Raft {
// 	for _, rf := range rafts {
// 		if rf.IsLeader() && !rf.IsDead() {
// 			return rf
// 		}
// 	}
// 	return nil
// }

// func main() {
// 	applyCh := make(chan raft.LogEntry)

// 	peers := []string{
// 		"node1",
// 		"node2",
// 		"node3",
// 	}

// 	r1 := raft.NewRaft(0, peers, applyCh)
// 	r2 := raft.NewRaft(1, peers, applyCh)
// 	r3 := raft.NewRaft(2, peers, applyCh)

// 	peerRaft := []*raft.Raft{
// 		r1,
// 		r2,
// 		r3,
// 	}
// 	// peerRaft[rand.Intn(3)].MakeLeader()

// 	r1.PeerRaft = peerRaft
// 	r2.PeerRaft = peerRaft
// 	r3.PeerRaft = peerRaft

// 	_ = r1
// 	_ = r2
// 	_ = r3

// 	fmt.Println("Raft cluster started")
// 	time.Sleep(10 * time.Second)
// }
