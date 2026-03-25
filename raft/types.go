package raft

const maxTimeout = 300

type Role string

const (
	Follower  Role = "follower"
	Leader    Role = "leader"
	Candidate Role = "candidate"
)

type LogEntry struct {
	Term     int
	LogIndex int
	Command  string
}

type RequestVoteArgs struct {
	Term         int
	CandidateID  int
	LastLogIndex int
	LastLogTerm  int
}

type RequestVoteReply struct {
	Term        int
	VoteGranted bool
}

type AppendEntriesArgs struct {
	Term         int
	LeaderID     int
	PrevLogIndex int
	PrevLogTerm  int
	Entries      []LogEntry
	LeaderCommit int
}

type AppendEntriesReply struct {
	Term    int
	Success bool
}
