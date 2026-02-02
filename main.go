package main

import (
	"example/raft/raft"
	"fmt"
	"time"
)

func main() {
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

	_ = r1
	_ = r2
	_ = r3

	fmt.Println("Raft cluster started")
	time.Sleep(10 * time.Second)
}
