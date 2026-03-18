package raft

import (
	"fmt"
	"net"
	"net/http"
	"net/rpc"
)

func (rf *Raft) StartServer(addr string) error {
	server := rpc.NewServer()
	err := server.Register(rf)

	if err != nil {
		fmt.Printf("Error registering RPC server: %v\n", err)
		return err
	}

	mux := http.NewServeMux()
	mux.Handle(rpc.DefaultRPCPath, server)

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Printf("Error starting RPC server: %v\n", err)
		return err
	}

	rf.listener = listener

	go http.Serve(listener, mux)

	return nil
}

func (rf *Raft) callPeer(peerAddr string, method string, args any, reply any) error {
	client, err := rpc.DialHTTP("tcp", peerAddr)
	if err != nil {
		fmt.Printf("Error dialing peer %s: %v\n", peerAddr, err)
		return err
	}

	defer client.Close()

	return client.Call(method, args, reply)
}
