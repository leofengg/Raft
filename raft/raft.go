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

	//testing
	dead bool
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
		dead:             false,
	}
	dummyLog := &LogEntry{term: 0}
	rf.logEntries = append(rf.logEntries, *dummyLog)

	go rf.ticker()
	return rf
}

func (rf *Raft) StartElection() {
	if rf.IsDead() {
		return
	}

	rf.mu.Lock()
	rf.role = string(Candidate)
	rf.votedFor = rf.id
	rf.votes = 1
	rf.currentTerm++
	fmt.Printf("Node %d: StartElection called, new term=%d\n", rf.id, rf.currentTerm)

	rf.mu.Unlock()
	req := &RequestVoteArgs{
		Term:         rf.currentTerm,
		CandidateID:  rf.id,
		LastLogIndex: len(rf.logEntries) - 1,                   //figure out what last log index is
		LastLogTerm:  rf.logEntries[len(rf.logEntries)-1].term, //figure out what last log term is
	}

	for i := range rf.PeerRaft {
		if rf.PeerRaft[i].id == rf.id && !rf.PeerRaft[i].IsDead() {
			continue
		}
		i := i
		go func() {
			reply := &RequestVoteReply{}
			rf.PeerRaft[i].HandleRequestVote(req, reply)

			rf.mu.Lock()
			defer rf.mu.Unlock()
			// fmt.Println(rf.role)
			if rf.role != string(Candidate) {
				return
			}

			fmt.Printf("Node %d: vote reply granted=%v votes=%d/%d\n",
				rf.id, reply.VoteGranted, rf.votes, len(rf.PeerRaft)/2+1)
			// fmt.Println("here", reply.VoteGranted)
			if reply.VoteGranted {
				rf.votes++
				if rf.votes >= len(rf.PeerRaft)/2+1 {
					rf.role = string(Leader)
					go rf.MakeLeader()
				}
				return
			}

		}()
	}

	// fmt.Println(rf.id, "starting election", rf.role)
}

func (rf *Raft) HandleRequestVote(req *RequestVoteArgs, reply *RequestVoteReply) {

	if rf.IsDead() {
		// fmt.Println(rf.id, "am dead")
		reply.VoteGranted = false
		return
	}
	// fmt.Println(rf.id, "raft recieving vote request from", req.CandidateID)
	rf.mu.Lock()
	defer rf.mu.Unlock()

	fmt.Printf("Node %d: got vote req from %d | myTerm=%d reqTerm=%d votedFor=%d\n",
		rf.id, req.CandidateID, rf.currentTerm, req.Term, rf.votedFor)

	if req.Term < rf.currentTerm {
		// fmt.Println(rf.id, req.CandidateID, "here2")
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

	alreadyVoted := rf.votedFor != -1 && rf.votedFor != req.CandidateID
	logOk := req.LastLogTerm > myLastTerm || (req.LastLogTerm == myLastTerm && req.LastLogIndex >= myLastIndex)

	if !alreadyVoted && logOk {
		reply.VoteGranted = true
	} else {
		// fmt.Println(rf.id, req.CandidateID, "here3")
		reply.VoteGranted = false
		return
	}

	rf.votedFor = req.CandidateID
	reply.Term = rf.currentTerm
	// fmt.Println(rf.id, "just voted for ", req.CandidateID, reply.VoteGranted)
	return
}

func (rf *Raft) HandleAppendEntries(args *AppendEntriesArgs) *AppendEntriesReply {
	rf.mu.Lock()

	reply := &AppendEntriesReply{Term: rf.currentTerm}

	if args.Term < rf.currentTerm {
		reply.Success = false
		rf.mu.Unlock()
		return reply
	}

	if args.Term > rf.currentTerm {
		rf.currentTerm = args.Term
		rf.votedFor = -1
	}

	rf.role = string(Follower)
	select {
	case rf.Heartbeat <- true:
	default:
	}

	if args.PrevLogIndex > 0 {
		if args.PrevLogIndex >= len(rf.logEntries) ||
			rf.logEntries[args.PrevLogIndex].term != args.PrevLogTerm {
			rf.mu.Unlock()
			reply.Success = false
			return reply
		}
	}

	if len(args.Entries) > 0 {
		rf.logEntries = append(rf.logEntries[:args.PrevLogIndex+1], args.Entries...)
	}

	apply := []LogEntry{}

	if args.LeaderCommit > rf.commitIndex {
		rf.commitIndex = min(args.LeaderCommit, len(rf.logEntries)-1)
		for rf.lastApplied < rf.commitIndex {
			rf.lastApplied++
			apply = append(apply, rf.logEntries[rf.lastApplied])
		}
	}

	reply.Success = true
	reply.Term = rf.currentTerm
	rf.mu.Unlock()

	for _, entry := range apply {
		rf.applyCh <- entry
	}

	return reply
}

func (rf *Raft) SubmitCommand(command string) bool {
	if rf.IsDead() || !rf.IsLeader() {
		return false
	}

	rf.mu.Lock()

	rf.logEntries = append(rf.logEntries, LogEntry{term: rf.currentTerm, Command: command})

	rf.mu.Unlock()
	var wg sync.WaitGroup

	for peer := range rf.PeerRaft {
		if rf.PeerRaft[peer].id != rf.id && !rf.PeerRaft[peer].IsDead() {
			// fmt.Println("sending heartbeat to", rf.PeerRaft[peer].id)

			rf.mu.Lock()
			prevIndex := rf.nextIndex[peer] - 1
			args := &AppendEntriesArgs{
				Term:         rf.currentTerm,
				LeaderID:     rf.id,
				LeaderCommit: rf.commitIndex,
			}
			args.PrevLogIndex = prevIndex
			args.PrevLogTerm = rf.logEntries[prevIndex].term
			args.Entries = rf.logEntries[prevIndex+1:]
			rf.mu.Unlock()

			wg.Add(1)

			go func(peer *Raft) {
				defer wg.Done()
				reply := peer.HandleAppendEntries(args)

				rf.mu.Lock()
				apply := []LogEntry{}
				if reply.Success {
					rf.matchIndex[peer.id] = len(rf.logEntries) - 1
					rf.nextIndex[peer.id] = len(rf.logEntries)
					apply = rf.incrementCommitIndex()
				} else {
					rf.nextIndex[peer.id]--
				}

				rf.mu.Unlock()
				for _, entry := range apply {
					rf.applyCh <- entry
				}

			}(rf.PeerRaft[peer])
		}
	}
	wg.Wait()

	return true
}

func (rf *Raft) incrementCommitIndex() []LogEntry {

	for n := len(rf.logEntries) - 1; n > rf.commitIndex; n-- {
		count := 1

		for peer := range rf.PeerRaft {
			if rf.PeerRaft[peer].id != rf.id && !rf.PeerRaft[peer].IsDead() {
				if rf.matchIndex[peer] >= n {
					count++
				}
			}
		}

		if count >= len(rf.PeerRaft)/2+1 {
			rf.commitIndex = n
			break
		}
	}

	var apply []LogEntry

	for rf.lastApplied < rf.commitIndex {
		rf.lastApplied++
		apply = append(apply, rf.logEntries[rf.lastApplied])
	}

	return apply

}

func (rf *Raft) MakeLeader() {
	rf.role = string(Leader)
	// fmt.Println(rf.id, "I AM THE LEADER")
	rf.matchIndex = make([]int, len(rf.PeerRaft))
	rf.nextIndex = make([]int, len(rf.PeerRaft))

	for i := range rf.PeerRaft {
		rf.nextIndex[i] = len(rf.logEntries)
		rf.matchIndex[i] = 0
	}

	rf.sendInitialHeartbeat()
}

func (rf *Raft) prepareHeartBeat() {
	// fmt.Println(rf.id, "preparing heartbeat send")
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
	// fmt.Println("SENDING INITIAL HEARTBEAT")
	var wg sync.WaitGroup
	args := &AppendEntriesArgs{
		Term:         rf.currentTerm,
		LeaderID:     rf.id,
		PrevLogIndex: 0,
		PrevLogTerm:  0,
		Entries:      nil,
		LeaderCommit: 0,
	}

	for peer := range rf.PeerRaft {
		if rf.PeerRaft[peer].id != rf.id && !rf.PeerRaft[peer].IsDead() {
			// fmt.Println("sending heartbeat to", rf.PeerRaft[peer].id)
			wg.Add(1)
			go func(peer *Raft) {
				defer wg.Done()
				rf.sendHeartbeat(peer, args)
			}(rf.PeerRaft[peer])
		}
	}
	wg.Wait()
}

func (rf *Raft) becomeFollower() {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	rf.role = string(Follower)
	rf.votes = 0
	fmt.Println("lost election, leader is: ")
}

func (rf *Raft) ticker() {
	for {

		if rf.IsDead() {
			return
		}

		switch rf.role {
		case string(Leader):
			time.Sleep(rf.heartbeatTimeout)
			rf.prepareHeartBeat()
		case string(Candidate):
			select {
			case <-time.After(rf.electionTimeout):
				rf.becomeFollower()
			}
		case string(Follower):
			rf.electionTimeout = randomTimeout()
			select {
			case <-rf.Heartbeat:
			case <-time.After(rf.electionTimeout):
				if !rf.IsDead() {
					go rf.StartElection()
				}
			}
		}
	}
}

// testing
func (rf *Raft) Kill() {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	rf.dead = true
	// fmt.Println("Raft", rf.id, "killed")
}

func (rf *Raft) IsDead() bool {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.dead
}

func (rf *Raft) IsLeader() bool {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if rf.role == string(Leader) {
		return true
	}
	return false
}

func (rf *Raft) Role() string {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.role
}

func (rf *Raft) Term() int {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.currentTerm
}

func (rf *Raft) ID() int {
	return rf.id
}
