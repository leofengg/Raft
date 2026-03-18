package main

import (
	"fmt"
	"time"

	"example/raft/raft"
)

// ---- helpers ----

var passed, failed int

func makeCluster(n int) ([]*raft.Raft, []chan raft.LogEntry) {
	// build peer address list
	peers := make([]string, n)
	for i := 0; i < n; i++ {
		peers[i] = fmt.Sprintf("localhost:%d", 9000+i)
	}

	applyChs := make([]chan raft.LogEntry, n)
	nodes := make([]*raft.Raft, n)

	for i := 0; i < n; i++ {
		applyChs[i] = make(chan raft.LogEntry, 100)
		nodes[i] = raft.NewRaft(i, peers, applyChs[i])
		fmt.Println(peers[i])
		if err := nodes[i].StartServer(peers[i]); err != nil {
			panic(fmt.Sprintf("failed to start server for node %d: %v", i, err))
		}
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

func pass(name string) {
	fmt.Printf("✅ PASS: %s\n", name)
	passed++
}

func fail(name, reason string) {
	fmt.Printf("❌ FAIL: %s — %s\n", name, reason)
	failed++
}

// ---- election tests ----

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

// ---- replication tests ----

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

func testReplicationBasic() {
	name := "TestReplicationBasic"
	nodes, applyChs := makeCluster(5)
	defer killAll(nodes)

	leader := waitForLeader(nodes, 5*time.Second)
	if leader == nil {
		fail(name, "no leader elected")
		return
	}
	time.Sleep(300 * time.Millisecond)

	leader.SubmitCommand("hello")

	for i, n := range nodes {
		if n.IsDead() {
			continue
		}
		entries := waitForApplied(applyChs[i], 1, 3*time.Second)
		if len(entries) == 0 {
			fail(name, fmt.Sprintf("node %d never applied the command", i))
			return
		}
		if entries[0].Command != "hello" {
			fail(name, fmt.Sprintf("node %d applied wrong command: %s", i, entries[0].Command))
			return
		}
	}
	pass(name)
}

func testReplicationWithDeadFollower() {
	name := "TestReplicationWithDeadFollower"
	nodes, applyChs := makeCluster(5)
	defer killAll(nodes)

	leader := waitForLeader(nodes, 5*time.Second)
	if leader == nil {
		fail(name, "no leader elected")
		return
	}
	time.Sleep(300 * time.Millisecond)

	killed := 0
	for _, n := range nodes {
		if !n.IsDead() && !n.IsLeader() && killed < 2 {
			fmt.Printf("   Killing follower Node %d\n", n.ID())
			n.Kill()
			killed++
		}
	}

	if !leader.SubmitCommand("quorum-cmd") {
		fail(name, "SubmitCommand returned false")
		return
	}

	appliedCount := 0
	for i, n := range nodes {
		if n.IsDead() {
			continue
		}
		entries := waitForApplied(applyChs[i], 1, 3*time.Second)
		if len(entries) > 0 && entries[0].Command == "quorum-cmd" {
			appliedCount++
			fmt.Printf("   Node %d applied\n", i)
		}
	}

	if appliedCount < 3 {
		fail(name, fmt.Sprintf("only %d nodes applied, expected at least 3", appliedCount))
		return
	}
	pass(name)
}

func testReplicationOrderGuarantee() {
	name := "TestReplicationOrderGuarantee"
	nodes, applyChs := makeCluster(5)
	defer killAll(nodes)

	leader := waitForLeader(nodes, 5*time.Second)
	if leader == nil {
		fail(name, "no leader elected")
		return
	}
	time.Sleep(300 * time.Millisecond)

	commands := []string{"first", "second", "third", "fourth", "fifth"}
	for _, cmd := range commands {
		if !leader.SubmitCommand(cmd) {
			fail(name, fmt.Sprintf("SubmitCommand failed for '%s'", cmd))
			return
		}
	}

	for i, n := range nodes {
		if n.IsDead() {
			continue
		}
		entries := waitForApplied(applyChs[i], len(commands), 5*time.Second)
		if len(entries) != len(commands) {
			fail(name, fmt.Sprintf("node %d got %d entries, expected %d", i, len(entries), len(commands)))
			return
		}
		for j, e := range entries {
			if e.Command != commands[j] {
				fail(name, fmt.Sprintf("node %d entry %d: expected '%s' got '%s'", i, j, commands[j], e.Command))
				return
			}
		}
		fmt.Printf("   Node %d: order correct\n", i)
	}
	pass(name)
}

func testReplicationAfterLeaderChange() {
	name := "TestReplicationAfterLeaderChange"
	nodes, applyChs := makeCluster(5)
	defer killAll(nodes)

	leader := waitForLeader(nodes, 5*time.Second)
	if leader == nil {
		fail(name, "no leader elected")
		return
	}
	time.Sleep(300 * time.Millisecond)

	if !leader.SubmitCommand("before-failover") {
		fail(name, "first SubmitCommand failed")
		return
	}

	fmt.Printf("   Killing leader Node %d\n", leader.ID())
	nodes[leader.ID()].Kill()

	newLeader := waitForLeader(nodes, 5*time.Second)
	if newLeader == nil {
		fail(name, "no new leader after failover")
		return
	}
	fmt.Printf("   New leader: Node %d term=%d\n", newLeader.ID(), newLeader.Term())
	time.Sleep(300 * time.Millisecond)

	if !newLeader.SubmitCommand("after-failover") {
		fail(name, "second SubmitCommand failed on new leader")
		return
	}

	for i, n := range nodes {
		if n.IsDead() {
			continue
		}
		entries := waitForApplied(applyChs[i], 2, 5*time.Second)
		if len(entries) < 2 {
			fail(name, fmt.Sprintf("node %d only got %d entries", i, len(entries)))
			return
		}
		if entries[0].Command != "before-failover" {
			fail(name, fmt.Sprintf("node %d entry 0: expected 'before-failover' got '%s'", i, entries[0].Command))
			return
		}
		if entries[1].Command != "after-failover" {
			fail(name, fmt.Sprintf("node %d entry 1: expected 'after-failover' got '%s'", i, entries[1].Command))
			return
		}
		fmt.Printf("   Node %d: both commands correct\n", i)
	}
	pass(name)
}

func testReplicationFailsWithoutQuorum() {
	name := "TestReplicationFailsWithoutQuorum"
	nodes, applyChs := makeCluster(5)
	defer killAll(nodes)

	leader := waitForLeader(nodes, 5*time.Second)
	if leader == nil {
		fail(name, "no leader elected")
		return
	}
	time.Sleep(300 * time.Millisecond)

	killed := 0
	for _, n := range nodes {
		if !n.IsDead() && !n.IsLeader() && killed < 3 {
			fmt.Printf("   Killing follower Node %d\n", n.ID())
			n.Kill()
			killed++
		}
	}

	leader.SubmitCommand("should-not-commit")

	time.Sleep(2 * time.Second)
	for i, n := range nodes {
		if n.IsDead() {
			continue
		}
		entries := waitForApplied(applyChs[i], 1, 200*time.Millisecond)
		if len(entries) > 0 {
			fail(name, fmt.Sprintf("node %d applied a command without quorum", i))
			return
		}
	}
	pass(name)
}

// ---- main ----

func main() {
	tests := []struct {
		name string
		fn   func()
	}{
		// election
		{"TestLeaderElected", testLeaderElected},
		{"TestOnlyOneLeader", testOnlyOneLeader},
		{"TestReElectionAfterLeaderDeath", testReElectionAfterLeaderDeath},
		{"TestNoLeaderWithoutQuorum", testNoLeaderWithoutQuorum},
		{"TestTermsOnlyIncrease", testTermsOnlyIncrease},
		// replication
		{"TestSubmitCommand", testSubmitCommand},
		{"TestSubmitMultipleCommands", testSubmitMultipleCommands},
		{"TestSubmitOnNonLeaderFails", testSubmitOnNonLeaderFails},
		{"TestReplicationBasic", testReplicationBasic},
		{"TestReplicationWithDeadFollower", testReplicationWithDeadFollower},
		{"TestReplicationOrderGuarantee", testReplicationOrderGuarantee},
		{"TestReplicationAfterLeaderChange", testReplicationAfterLeaderChange},
		{"TestReplicationFailsWithoutQuorum", testReplicationFailsWithoutQuorum},
	}

	fmt.Println("=== Running Raft Tests ===")
	fmt.Println()

	for _, test := range tests {
		fmt.Printf("--- %s\n", test.name)
		test.fn()
		fmt.Println()
		time.Sleep(500 * time.Millisecond) // let ports free up between tests
	}

	fmt.Printf("=== Results: %d passed, %d failed ===\n", passed, failed)
}
