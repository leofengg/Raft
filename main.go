package main

import (
	"fmt"
	"math/rand"
	"time"

	"example/raft/raft"
)

func main() {
	rand.Seed(time.Now().UnixNano())

	const (
		numNodes      = 5
		killRounds    = 6
		roundInterval = 3 * time.Second
	)

	applyCh := make(chan raft.LogEntry)

	// Create Raft nodes
	rafts := make([]*raft.Raft, numNodes)
	for i := 0; i < numNodes; i++ {
		rafts[i] = raft.NewRaft(i, nil, applyCh)
	}

	// Wire peers
	for i := 0; i < numNodes; i++ {
		rafts[i].PeerRaft = rafts
	}

	fmt.Println("🚀 Raft cluster started with", numNodes, "nodes")

	time.Sleep(3 * time.Second)

	for round := 1; round <= killRounds; round++ {
		fmt.Printf("\n🔥 CHAOS ROUND %d 🔥\n", round)

		printClusterState(rafts)

		victim := chooseRandomAlive(rafts)
		if victim == nil {
			fmt.Println("❌ No alive nodes left")
			break
		}

		fmt.Printf("💀 Killing node %d (role=%s)\n", victim.ID(), victim.Role())
		victim.Kill()

		time.Sleep(roundInterval)
	}

	fmt.Println("\n🧪 Chaos test complete")
	select {}
}

func chooseRandomAlive(rafts []*raft.Raft) *raft.Raft {
	alive := []*raft.Raft{}
	for _, rf := range rafts {
		if !rf.IsDead() {
			alive = append(alive, rf)
		}
	}

	if len(alive) == 0 {
		return nil
	}

	return alive[rand.Intn(len(alive))]
}

func printClusterState(rafts []*raft.Raft) {
	fmt.Println("Cluster state:")
	for _, rf := range rafts {
		status := "alive"
		if rf.IsDead() {
			status = "dead"
		}

		fmt.Printf(
			"  Node %d | role=%s | term=%d | %s\n",
			rf.ID(),
			rf.Role(),
			rf.Term(),
			status,
		)
	}
}

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
