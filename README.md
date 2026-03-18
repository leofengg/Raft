# Raft Consensus Algorithm — Go Implementation

A from-scratch implementation of the [Raft consensus algorithm](https://raft.github.io/raft.pdf) in Go, including leader election, log replication, and RPC-based node communication.

---

## What is Raft?

Raft is a consensus algorithm that allows a cluster of nodes to agree on a sequence of values even in the presence of failures, as long as a majority of nodes remain alive. One node is elected leader and coordinates all writes — followers replicate the leader's log and apply committed entries to their state machines.

---

## Project Structure

```
raft/
├── raft.go       # Core Raft logic — election, replication, ticker
├── rpc.go        # RPC server setup and peer communication
├── types.go      # Structs — Raft, LogEntry, RPC args/replies
└── utils.go      # randomTimeout and other helpers
main.go           # Test suite
```

---

## How It Works

### Leader Election

Each node starts as a follower with a randomized election timeout (800–1100ms). If no heartbeat arrives before the timeout fires, the node becomes a candidate, increments its term, and sends `RequestVote` RPCs to all peers. A candidate wins if it receives votes from a majority (`n/2 + 1`). On winning it immediately sends heartbeats to suppress other candidates.

Randomized timeouts prevent simultaneous elections. Term numbers act as logical clocks — any node that sees a higher term immediately steps down to follower. A vote is only granted if the candidate's log is at least as up-to-date as the voter's, preventing stale nodes from winning.

### Log Replication

The leader accepts commands via `SubmitCommand`, appends to its local log, then sends `AppendEntries` RPCs to all followers in parallel. Once a majority acknowledges an entry the leader advances `commitIndex` and delivers the entry to `applyCh` for the application to consume. Followers learn entries are committed via the `LeaderCommit` field in the next `AppendEntries`.

The leader tracks `nextIndex` per follower and only sends entries each peer is missing. Followers reject appends if `PrevLogIndex/PrevLogTerm` doesn't match their log, guaranteeing consistency.

### RPC Communication

Nodes communicate via Go's `net/rpc` over TCP. Peer addresses are passed in at startup as a static list. `net/rpc` was chosen over gRPC for simplicity — no protobuf schema required, sufficient for a Go-only cluster.

---

## Usage

```go
peers := []string{
    "localhost:9000",
    "localhost:9001",
    "localhost:9002",
}

applyCh := make(chan raft.LogEntry, 100)
rf := raft.NewRaft(id, peers, applyCh)
rf.StartServer(peers[id])

// listen for committed entries
go func() {
    for entry := range applyCh {
        fmt.Printf("applied: %s\n", entry.Command)
    }
}()

// submit a command (must be called on the leader)
if rf.IsLeader() {
    rf.SubmitCommand("set x=1")
}
```

---

## Limitations

- **No persistence** — state is lost on crash; not yet safe across restarts
- **No log compaction** — log grows unbounded
- **No membership changes** — cluster topology is fixed at startup

---

## References

- [Raft paper](https://raft.github.io/raft.pdf) — Ongaro & Ousterhout, 2014
- [Raft visualization](https://raft.github.io)
- [MIT 6.824 Distributed Systems](https://pdos.csail.mit.edu/6.824/)
