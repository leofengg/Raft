package raft

const maxTimeout = 300

type Role string

const (
	Follower  Role = "follower"
	Leader    Role = "leader"
	Candidate Role = "candidate"
)

type LogEntry struct {
	term     int
	logIndex int
	command  string
}
