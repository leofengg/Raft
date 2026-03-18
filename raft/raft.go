package raft

import (
	"net"
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

	//channels
	Heartbeat    chan bool
	BecameLeader chan bool

	//testing
	dead bool

	//network
	listener net.Listener
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
	dummyLog := &LogEntry{Term: 0}
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

	rf.mu.Unlock()
	req := &RequestVoteArgs{
		Term:         rf.currentTerm,
		CandidateID:  rf.id,
		LastLogIndex: len(rf.logEntries) - 1,                   //figure out what last log index is
		LastLogTerm:  rf.logEntries[len(rf.logEntries)-1].Term, //figure out what last log term is
	}

	for i, addr := range rf.peers {
		if i == rf.id {
			continue
		}

		addr := addr

		go func() {
			reply := &RequestVoteReply{}

			err := rf.callPeer(addr, "Raft.HandleRequestVote", req, reply)
			if err != nil {
				return
			}

			rf.mu.Lock()
			defer rf.mu.Unlock()
			if rf.role != string(Candidate) {
				return
			}

			if reply.VoteGranted {
				rf.votes++
				if rf.votes >= len(rf.peers)/2+1 {
					rf.role = string(Leader)
					go rf.MakeLeader()
				}
				return
			}

		}()
	}
}

func (rf *Raft) HandleRequestVote(req *RequestVoteArgs, reply *RequestVoteReply) error {

	rf.mu.Lock()
	defer rf.mu.Unlock()

	if req.Term < rf.currentTerm {
		reply.VoteGranted = false
		return nil
	}

	if req.Term > rf.currentTerm {
		rf.currentTerm = req.Term
		rf.votedFor = -1
		rf.role = string(Follower)
	}

	myLastIndex := len(rf.logEntries) - 1
	myLastTerm := 0

	if myLastIndex >= 0 {
		myLastTerm = rf.logEntries[myLastIndex].Term
	}

	alreadyVoted := rf.votedFor != -1 && rf.votedFor != req.CandidateID
	logOk := req.LastLogTerm > myLastTerm || (req.LastLogTerm == myLastTerm && req.LastLogIndex >= myLastIndex)

	if !alreadyVoted && logOk {
		reply.VoteGranted = true
	} else {
		reply.VoteGranted = false
		return nil
	}

	rf.votedFor = req.CandidateID
	reply.Term = rf.currentTerm
	return nil
}

func (rf *Raft) HandleAppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) error {
	rf.mu.Lock()

	if args.Term < rf.currentTerm {
		reply.Success = false
		rf.mu.Unlock()
		return nil
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
			rf.logEntries[args.PrevLogIndex].Term != args.PrevLogTerm {
			rf.mu.Unlock()
			reply.Success = false
			return nil
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

	return nil
}

func (rf *Raft) SubmitCommand(command string) bool {
	if !rf.IsLeader() {
		return false
	}

	rf.mu.Lock()

	rf.logEntries = append(rf.logEntries, LogEntry{Term: rf.currentTerm, Command: command})

	rf.mu.Unlock()
	var wg sync.WaitGroup

	for i, addr := range rf.peers {
		i, addr := i, addr

		if i != rf.id {
			rf.mu.Lock()
			prevIndex := rf.nextIndex[i] - 1
			args := &AppendEntriesArgs{
				Term:         rf.currentTerm,
				LeaderID:     rf.id,
				LeaderCommit: rf.commitIndex,
			}
			args.PrevLogIndex = prevIndex
			args.PrevLogTerm = rf.logEntries[prevIndex].Term
			args.Entries = rf.logEntries[prevIndex+1:]
			rf.mu.Unlock()

			wg.Add(1)

			go func(i int) {
				defer wg.Done()

				reply := &AppendEntriesReply{}

				err := rf.callPeer(addr, "Raft.HandleAppendEntries", args, reply)
				if err != nil {
					return
				}

				rf.mu.Lock()
				apply := []LogEntry{}
				if reply.Success {
					rf.matchIndex[i] = len(rf.logEntries) - 1
					rf.nextIndex[i] = len(rf.logEntries)
					apply = rf.incrementCommitIndex()
				} else {
					rf.nextIndex[i]--
				}

				rf.mu.Unlock()
				for _, entry := range apply {
					rf.applyCh <- entry
				}

			}(i)
		}
	}
	wg.Wait()

	return true
}

func (rf *Raft) incrementCommitIndex() []LogEntry {

	for n := len(rf.logEntries) - 1; n > rf.commitIndex; n-- {
		count := 1

		for i := range rf.peers {
			if i != rf.id {
				if rf.matchIndex[i] >= n {
					count++
				}
			}
		}

		if count >= len(rf.peers)/2+1 {
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
	rf.matchIndex = make([]int, len(rf.peers))
	rf.nextIndex = make([]int, len(rf.peers))

	for i := range rf.peers {
		rf.nextIndex[i] = len(rf.logEntries)
		rf.matchIndex[i] = 0
	}

	rf.sendInitialHeartbeat()
}

func (rf *Raft) prepareHeartBeat() {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	for i, addr := range rf.peers {
		i, addr := i, addr
		if i != rf.id {

			args := &AppendEntriesArgs{
				Term:         rf.currentTerm,
				LeaderID:     rf.id,
				PrevLogIndex: len(rf.logEntries) - 1,
				PrevLogTerm:  rf.logEntries[len(rf.logEntries)-1].Term,
				Entries:      rf.logEntries[rf.nextIndex[i]:], //send all log entries after dummy log
				LeaderCommit: rf.commitIndex,
			}
			go func(id int) {
				reply := &AppendEntriesReply{}

				err := rf.callPeer(addr, "Raft.HandleAppendEntries", args, reply)

				if err != nil {
					return
				}
				rf.mu.Lock()

				if reply.Success {
					rf.matchIndex[id] = len(rf.logEntries) - 1
					rf.nextIndex[id] = len(rf.logEntries)
				} else {
					if rf.nextIndex[id] > 1 {
						rf.nextIndex[id]--
					}
				}

				rf.mu.Unlock()

			}(i)
		}
	}
}

func (rf *Raft) sendInitialHeartbeat() {
	var wg sync.WaitGroup
	args := &AppendEntriesArgs{
		Term:         rf.currentTerm,
		LeaderID:     rf.id,
		PrevLogIndex: 0,
		PrevLogTerm:  0,
		Entries:      nil,
		LeaderCommit: 0,
	}

	for i, addr := range rf.peers {
		i, addr := i, addr
		if i != rf.id {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				reply := &AppendEntriesReply{}
				err := rf.callPeer(addr, "Raft.HandleAppendEntries", args, reply)
				if err != nil {
					return
				}
			}(i)
		}
	}
	wg.Wait()
}

func (rf *Raft) becomeFollower() {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	rf.role = string(Follower)
	rf.votes = 0
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

func (rf *Raft) Kill() {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	rf.dead = true
	if rf.listener != nil {
		rf.listener.Close() // stops accepting new connections
	}
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
