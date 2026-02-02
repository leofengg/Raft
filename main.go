package main

import (
	"fmt"
	"time"

	"example/raft/raft"
)

func main() {
	const n = 3
	applyCh := make(chan raft.LogEntry)

	peers := []string{
		"node1",
		"node2",
		"node3",
	}

	r1 := raft.NewRaft(0, peers, applyCh)
	r2 := raft.NewRaft(1, peers, applyCh)
	r3 := raft.NewRaft(2, peers, applyCh)

	peerRaft := []*raft.Raft{
		r1,
		r2,
		r3,
	}
	// peerRaft[rand.Intn(3)].MakeLeader()

	r1.PeerRaft = peerRaft
	r2.PeerRaft = peerRaft
	r3.PeerRaft = peerRaft

	fmt.Println("🚀 Raft cluster started")
	// Let elections happen
	time.Sleep(3 * time.Second)

	leader := findLeader(peerRaft)
	if leader == nil {
		fmt.Println("❌ No leader elected")
		return
	}

	fmt.Println("👑 Initial leader:", leader.ID())

	// Kill the leader
	fmt.Println("😴 Putting leader to sleep...")
	leader.Kill()

	// Wait for re-election
	time.Sleep(3 * time.Second)

	newLeader := findLeader(peerRaft)
	if newLeader == nil {
		fmt.Println("❌ No new leader elected")
		return
	}

	fmt.Println("👑 New leader elected:", newLeader.ID())

	fmt.Println("✅ Election robustness test complete")
	return
}

func findLeader(rafts []*raft.Raft) *raft.Raft {
	for _, rf := range rafts {
		if rf.IsLeader() && !rf.IsDead() {
			return rf
		}
	}
	return nil
}

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
