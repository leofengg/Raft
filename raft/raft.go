package raft

import (
	"fmt"
	"sync"
	"time"
)

type Raft struct {
	mu    sync.Mutex
	peers []string //address for other nodes

	//raft info
	id          int
	currentTerm int
	votes       int
	logEntries  []LogEntry
	votedFor    int

	role        string
	commitIndex int
	lastApplied int

	nextIndex  []int
	matchIndex []int

	applyCh chan LogEntry

	//timers
	heartbeatTimeout time.Duration
	electionTimeout  time.Duration

	PeerRaft []*Raft

	//channels
	Heartbeat    chan bool
	BecameLeader chan bool
}

func NewRaft(id int, peers []string, applyCh chan LogEntry) *Raft {

	rf := &Raft{
		id:               id,
		peers:            peers,
		applyCh:          applyCh,
		heartbeatTimeout: time.Millisecond * 100,
		electionTimeout:  randomTimeout(),
		role:             string(Follower),
		currentTerm:      0,
		Heartbeat:        make(chan bool, 1),
		BecameLeader:     make(chan bool, 1),
	}
	dummyLog := &LogEntry{term: 0}
	rf.logEntries = append(rf.logEntries, *dummyLog)

	go rf.ticker()
	return rf
}

func (rf *Raft) StartElection() {

	rf.role = string(Candidate)
	req := &RequestVoteArgs{
		Term:         rf.currentTerm,
		CandidateID:  rf.id,
		LastLogIndex: len(rf.logEntries) - 1,                   //figure out what last log index is
		LastLogTerm:  rf.logEntries[len(rf.logEntries)-1].term, //figure out what last log term is
	}

	for i := range rf.peers {
		if rf.PeerRaft[i].id == rf.id {
			continue
		}

		go func() {
			reply := &RequestVoteReply{}
			rf.PeerRaft[i].HandleRequestVote(req, reply)

			rf.mu.Lock()
			defer rf.mu.Unlock()
			fmt.Println(rf.role)
			if rf.role != string(Candidate) {
				return
			}
			fmt.Println("here", reply.VoteGranted)
			if reply.VoteGranted {
				rf.votes++
				if rf.votes > len(rf.peers)/(2+1) {
					rf.BecameLeader <- true
				}
				return
			}

		}()
	}

	fmt.Println(rf.id, "starting election", rf.role)
}

func (rf *Raft) sendRequestVote(req *RequestVoteArgs, reply *RequestVoteReply) {
	for i := 0; i < len(rf.peers)-1; i++ {
		if rf.PeerRaft[i].id != rf.id {
			go rf.PeerRaft[i].HandleRequestVote(req, reply)
		}
	}
}

func (rf *Raft) HandleRequestVote(req *RequestVoteArgs, reply *RequestVoteReply) {
	fmt.Println(rf.id, "raft recieving vote request from", req.CandidateID)
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if req.Term < rf.currentTerm {
		fmt.Println(rf.id, req.CandidateID, "here2")
		reply.VoteGranted = false
		return
	}

	if req.Term > rf.currentTerm {
		rf.currentTerm = req.Term
		rf.votedFor = -1
		rf.role = string(Follower)
	}

	myLastIndex := len(rf.logEntries) - 1
	myLastTerm := 0

	if myLastIndex >= 0 {
		myLastTerm = rf.logEntries[myLastIndex].term
	}

	if (req.LastLogIndex >= myLastIndex && req.LastLogTerm == myLastTerm) || req.LastLogTerm > myLastTerm {
		reply.VoteGranted = true
	} else {
		fmt.Println(rf.id, req.CandidateID, "here3")
		reply.VoteGranted = false
		return
	}

	rf.votedFor = req.CandidateID
	reply.Term = rf.currentTerm
	fmt.Println(rf.id, "just voted for ", req.CandidateID, reply.VoteGranted)
	return
}

func (rf *Raft) HandleAppendEntries(args *AppendEntriesArgs) *AppendEntriesReply {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	fmt.Println(rf.id, "HANDLING APPEND ENTRIES ON FIRST HEARTBEAT", rf.role)
	reply := &AppendEntriesReply{Term: rf.currentTerm}
	if args.Term < rf.currentTerm {
		reply.Success = false
		return reply
	}

	if args.Term > rf.currentTerm {
		rf.currentTerm = args.Term
		rf.votedFor = -1
	}

	rf.role = string(Follower)
	reply.Success = true
	reply.Term = rf.currentTerm

	rf.Heartbeat <- true

	return reply
}

func (rf *Raft) MakeLeader() {
	rf.role = string(Leader)
	fmt.Println(rf.id, "I AM THE LEADER")
	rf.sendInitialHeartbeat()
}

func (rf *Raft) prepareHeartBeat() {
	fmt.Println(rf.id, "preparing heartbeat send")
	rf.mu.Lock()
	defer rf.mu.Unlock()
	args := &AppendEntriesArgs{
		Term:         rf.currentTerm,
		LeaderID:     rf.id,
		PrevLogIndex: 0,
		PrevLogTerm:  0,
		Entries:      nil,
		LeaderCommit: 0,
	}

	for peer := range rf.PeerRaft {
		if rf.PeerRaft[peer].id != rf.id {
			go rf.sendHeartbeat(rf.PeerRaft[peer], args)
		}
	}
}

func (rf *Raft) sendHeartbeat(peer *Raft, args *AppendEntriesArgs) {
	peer.HandleAppendEntries(args)
}

func (rf *Raft) sendInitialHeartbeat() {
	fmt.Println("SENDING INITIAL HEARTBEAT")
	args := &AppendEntriesArgs{
		Term:         rf.currentTerm,
		LeaderID:     rf.id,
		PrevLogIndex: 0,
		PrevLogTerm:  0,
		Entries:      nil,
		LeaderCommit: 0,
	}

	for peer := range rf.PeerRaft {
		if rf.PeerRaft[peer].id != rf.id {
			go rf.sendHeartbeat(rf.PeerRaft[peer], args)
		}
	}
}

func (rf *Raft) ticker() {
	for {
		switch rf.role {
		case string(Leader):
			time.Sleep(rf.heartbeatTimeout)
			rf.prepareHeartBeat()
		case string(Candidate):
			select {
			case <-rf.BecameLeader:
				rf.mu.Lock()
				rf.MakeLeader()
				rf.mu.Unlock()
			}
		case string(Follower):
			select {
			case <-rf.Heartbeat:
			case <-time.After(rf.electionTimeout):
				go rf.StartElection()
			}
		}
	}
}
