package raft

import (
	"fmt"
	"math/rand"
	"time"
)

func randomTimeout() time.Duration {
	return time.Duration(800+rand.Intn(maxTimeout)) * time.Millisecond
}

func (rf *Raft) runElectionTimer() {
	for {
		timeout := rf.electionTimeout
		time.Sleep(timeout)

		rf.mu.Lock()
		if rf.role == string(Leader) {
			fmt.Println(rf.id, "just became leader")
			rf.mu.Unlock()
			continue
		}

		rf.role = string(Candidate)
		rf.currentTerm++
		rf.votedFor = rf.id
		rf.mu.Unlock()
		go rf.StartElection()
	}
}
