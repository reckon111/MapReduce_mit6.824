package raft

// 快照作用：机器快速恢复，压缩raft日志
// 快照和raft持久化信息
// 机器创建快照时会改变raft持久化信息（raft日志), 快照与raft持久化信息一一对应
// 机器重新恢复时，机器首先安装快照(包含raft信息和机器状态)，raft在读入持久化信息

//
// this is an outline of the API that raft must expose to
// the service (or tester). see comments below for
// each of these functions for more details.
//
// rf = Make(...)
//   create a new Raft server.
// rf.Start(command interface{}) (index, term, isleader)
//   start agreement on a new log entry
// rf.GetState() (term, isLeader)
//   ask a Raft for its current term, and whether it thinks it is leader
// ApplyMsg
//   each time a new entry is committed to the log, each Raft peer
//   should send an ApplyMsg to the service (or tester)
//   in the same server.
//

import (
//	"bytes"
	"sync"
	"sync/atomic"

	"6.824/labgob"
	"6.824/labrpc"
	
	"time"
	"math/rand"
	// "fmt"
	"log"

	"bytes"
	"encoding/gob"
)

func init() {
	log.SetFlags(log.Lmicroseconds)
}

const (
	sendHeartbeatTime =  200 * time.Millisecond
	left = 400
	right = 800
)

//
// as each Raft peer becomes aware that successive log entries are
// committed, the peer should send an ApplyMsg to the service (or
// tester) on the same server, via the applyCh passed to Make(). set
// CommandValid to true to indicate that the ApplyMsg contains a newly
// committed log entry.
//
// in part 2D you'll want to send other kinds of messages (e.g.,
// snapshots) on the applyCh, but set CommandValid to false for these
// other uses.
//
type ApplyMsg struct {
	CommandValid bool
	Command      interface{}
	CommandIndex int

	// For 2D:
	SnapshotValid bool
	Snapshot      []byte  // 记录状态机状态信息，此处为SnapshotIndex的命令
	SnapshotTerm  int
	SnapshotIndex int
}

// 要序列化的结构体域都要大写字母开头,否则序列化时将被忽略
type Entry struct {
	Term int 
	Command      interface{}
}

//
// A Go object implementing a single Raft peer.
//
type Raft struct {
	mu        sync.Mutex          // Lock to protect shared access to this peer's state
	peers     []*labrpc.ClientEnd // RPC end points of all peers
	persister *Persister          // Object to hold this peer's persisted statereadPersiststa
	me        int                 // this peer's index into peers[]
	dead      int32               // set by Kill()

	// Your data here (2A, 2B, 2C).
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.

	status string // raft 服务器的状态：leader, follower, candidate

	// 需要持久化的状态信息
	currentTerm int
	votedFor int 
	votedTerm int
	log []Entry
	lastIncludedTerm int  // 保存的最新快照的信息，需要持久化
	lastIncludedIndex int
	// logStartIndex int
	// logEndIndex int
	
	lastTime time.Time
	electionTime time.Duration

	commitIndex int 
	lastApplied int 
	largestIndexMathWithLeader int // **关键代码**: 防止leader并发追加时, follower中长日志被短日志覆盖

	nextIndex []int
	matchIndex []int
	leaderId int

	newCommitIndex chan int
	applyCh chan ApplyMsg
	startSendHeartBeats chan bool
	startElection chan bool
	chStartReplicted chan bool
}

// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {

	var term int
	var isleader bool
	// Your code here (2A).
	rf.mu.Lock()
	defer rf.mu.Unlock()
	term = rf.currentTerm;
	if rf.status == "leader" {
		isleader = true;
	}

	return term, isleader
}

//
// save Raft's persistent state to stable storage,
// where it can later be retrieved after a crash and restart.
// see paper's Figure 2 for a description of what should be persistent.
//
func (rf *Raft) persist() {
	// Your code here (2C).
	// Example:
	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	e.Encode(rf.currentTerm)
	e.Encode(rf.votedFor)
	e.Encode(rf.votedTerm)
	e.Encode(rf.log)
	e.Encode(rf.lastIncludedTerm)
	e.Encode(rf.lastIncludedIndex)
	data := w.Bytes()
	rf.persister.SaveRaftState(data)
}


//
// restore previously persisted state.
//
func (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
	// Your code here (2C).
	// Example:
	r := bytes.NewBuffer(data)
	d := labgob.NewDecoder(r)
	var currentTerm int 
	var votedFor int 
	var votedTerm int
	var restortedLog []Entry
	var lastIncludedTerm int
	var lastIncludedIndex int
	if d.Decode(&currentTerm) != nil ||
	   d.Decode(&votedFor) != nil || 
	   d.Decode(&votedTerm) != nil ||
	   d.Decode(&restortedLog) != nil ||
	   d.Decode(&lastIncludedTerm) != nil ||
	   d.Decode(&lastIncludedIndex) != nil {
		log.Fatalln("Decode err")
	} else {
		rf.currentTerm = currentTerm
		rf.votedFor = votedFor
		rf.votedTerm = votedTerm
		rf.log = restortedLog
		rf.lastIncludedTerm = lastIncludedTerm
		rf.lastIncludedIndex = lastIncludedIndex
	}
}


//
// leader发送一个快照到此节点，节点首先将快照信息发送给service
// service 调用此函数，将快照信息发送给raft,
// raft判断service是否可以安装快照：可以安装该快照则改变raft持久化状态，返回true，service会安装该快照成功转为快照状态; 否则返回false,

// A service wants to switch to snapshot.  Only do so if Raft hasn't
// have more recent info since it communicate the snapshot on applyCh.
//
func (rf *Raft) CondInstallSnapshot(lastIncludedTerm int, lastIncludedIndex int, snapshot []byte) bool {
	// Your code here (2D).

	rf.mu.Lock()
	defer rf.mu.Unlock()

	
	if rf.commitIndex >= lastIncludedIndex || rf.lastIncludedIndex >= lastIncludedIndex {
		//log.Printf("server %d 任期 %d, 不安装快照\n", rf.me, rf.currentTerm)
		return false
	}

	//log.Printf("server %d 任期 %d, 安装快照\n", rf.me, rf.currentTerm)
	r := bytes.NewBuffer(snapshot)
	d := labgob.NewDecoder(r)
	var cmd int
	if d.Decode(&cmd) != nil {
		log.Fatalf("decode error\n")
	}

	entry := Entry {
		Term: lastIncludedTerm,
		Command: cmd,
	}
	rf.log = []Entry{
		entry,
	}
	rf.lastIncludedTerm = lastIncludedTerm
	rf.lastIncludedIndex = lastIncludedIndex 
	rf.commitIndex = lastIncludedIndex 
	rf.lastApplied = lastIncludedIndex
	rf.persist();  

	return true
}

// 已应用的命令达到10，service将创建快照，并调用该函数，通知raft程序 使其修改raft状态

// the service says it has created a snapshot that has
// all info up to and including index. this means the
// service no longer needs the log through (and including)
// that index. Raft should now trim its log as much as possible.
func (rf *Raft) Snapshot(index int, snapshot []byte) {
	// Your code here (2D).
	go func() {
		rf.mu.Lock()
		defer rf.mu.Unlock()
		if index <= rf.lastIncludedIndex {
			return
		}
		//log.Printf("server %d 快照被service调用, 创建的位置的绝对索引为%d, 相对位置为%d\n", rf.me, index, rf.getRelativeIndex(index))
		rf.lastIncludedTerm = rf.log[rf.getRelativeIndex(index)].Term
		b := serialEntries(rf.log[rf.getRelativeIndex(index): ])
		rf.log = d_serialEntries(b);
		rf.lastIncludedIndex = index 
	
		rf.persist();   
		//log.Printf("创建快照成功, server %d status %s lastIncludedIndex %d \n新日志为%v\n", rf.me, rf.status, rf.lastIncludedIndex, rf.log)
	}()
}


type InstallSnapshotArgs struct {
	Term int 
	LeaderId int
	LastIncludedIndex int
	LastIncludedTerm int
	Snapshot []byte 
}

type InstallSnapshotReply struct {
	Term int 
}

func (rf *Raft) InstallSnapshot(args *InstallSnapshotArgs, reply *InstallSnapshotReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	//log.Printf("安装快照被调用, server %d status %s 任期 %d, leader %d\n", rf.me, rf.status, rf.currentTerm, args.LeaderId)
	reply.Term = rf.currentTerm

	if args.Term < rf.currentTerm {
		// rf.mu.Unlock()
		return
	}

	if args.Term > rf.currentTerm {  // 碰到更新的任期, 无条件更新任期, 并转为follower
		rf.currentTerm = args.Term
		rf.persist()
	}
	rf.status = "follower"
	rf.lastTime = time.Now()
	rf.leaderId = args.LeaderId

	// 发送快照信息给状态机
	applyMsg := ApplyMsg{
		SnapshotValid: true,
		Snapshot: args.Snapshot,
		SnapshotTerm: args.LastIncludedTerm,
		SnapshotIndex: args.LastIncludedIndex,
	}
	// rf.mu.Unlock()

	go func(applyMsg ApplyMsg) {
		rf.applyCh <- applyMsg 
	}(applyMsg)
	
}

func (rf *Raft) sendInstallSnapshot(server int, args *InstallSnapshotArgs, reply *InstallSnapshotReply) bool {
	ok := rf.peers[server].Call("Raft.InstallSnapshot", args, reply)
	return ok
}

//
// example RequestVote RPC arguments structure.
// field names must start with capital letters!
//
type RequestVoteArgs struct {
	// Your data here (2A, 2B).
	Term int 
	CandidateId int
	LastLogIndex int
	LastLogTerm int 
}

//
// example RequestVote RPC reply structure.
// field names must start with capital letters!
//
type RequestVoteReply struct {
	// Your data here (2A).
	Term int 
	VoteGranted bool 
}

//
// example RequestVote RPC handler.
//
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here (2A, 2B).
	rf.mu.Lock()
	defer rf.mu.Unlock()
	reply.Term = rf.currentTerm
	reply.VoteGranted = false

	//log.Printf("server %d status %s 任期 %d, 收到 server %d 任期 %d 的投票请求\n", rf.me, rf.status, rf.currentTerm, args.CandidateId, args.Term)

	if args.Term == rf.currentTerm { // candidate 任期相同可能投票
		// candidate日志不落后, 且此server在这一任期内未投过票
		if !rf.isAdvanceThanCandidate(args.LastLogTerm, args.LastLogIndex) && rf.votedTerm < args.Term { 
			rf.votedFor = args.CandidateId
			rf.votedTerm = args.Term
			rf.lastTime = time.Now()
			rf.status = "follower"
			rf.leaderId = args.CandidateId
			rf.largestIndexMathWithLeader = 0
			reply.VoteGranted = true
			rf.persist()
		}
	} else if args.Term > rf.currentTerm {  // 碰到更新的任期, 无条件更新任期, 并转为follower
		rf.currentTerm = args.Term
		rf.status = "follower"
		if !rf.isAdvanceThanCandidate(args.LastLogTerm, args.LastLogIndex) { // candidate日志不落后, 投票, 在投票之后重置选举时间
			rf.votedFor = args.CandidateId
			rf.votedTerm = args.Term
			rf.lastTime = time.Now()
			rf.leaderId = args.CandidateId
			rf.largestIndexMathWithLeader = 0
			reply.VoteGranted = true
		}
		rf.persist()
	}
}

func (rf *Raft) isAdvanceThanCandidate(LastLogTerm int, LastLogIndex int) bool {
	tail := len(rf.log) - 1

	if rf.log[tail].Term > LastLogTerm { return true }
	if rf.log[tail].Term < LastLogTerm { return false }
	return rf.getAbsoluteIndex(tail) > LastLogIndex
}

//
// example code to send a RequestVote RPC to a server.
// server is the index of the target server in rf.peers[].
// expects RPC arguments in args.
// fills in *reply with RPC reply, so caller should
// pass &reply.
// the types of the args and reply passed to Call() must be
// the same as the types of the arguments declared in the
// handler function (including whether they are pointers).
//
// The labrpc package simulates a lossy network, in which servers
// may be unreachable, and in which requests and replies may be lost.
// Call() sends a request and waits for a reply. If a reply arrives
// within a timeout interval, Call() returns true; otherwise
// Call() returns false. Thus Call() may not return for a while.
// A false return can be caused by a dead server, a live server that
// can't be reached, a lost request, or a lost reply.
//
// Call() is guaranteed to return (perhaps after a delay) *except* if the
// handler function on the server side does not return.  Thus there
// is no need to implement your own timeouts around Call().
//
// look at the comments in ../labrpc/labrpc.go for more details.
//
// if you're having trouble getting RPC to work, check that you've
// capitalized all field names in structs passed over RPC, and
// that the caller passes the address of the reply struct with &, not
// the struct itself.
//
func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	return ok
}


type AppendEntriesArgs struct {
	// Your data here (2A, 2B).
	Term int 
	LeaderId int
	PrevLogIndex int
	PrevLogTerm int
	Entries []byte
	LeaderCommit int
}

type AppendEntriesReply struct {
	// Your data here (2A).
	Term int 
	Success bool 
	NewPrevLogIndex int
}

func (rf *Raft) isMatchPrevLog(PrevLogIndex int, PrevLogTerm int) (bool, int) {
	NewPrevLogIndex := PrevLogIndex
	if PrevLogIndex >= rf.getAbsoluteIndex(len(rf.log)) { 
		NewPrevLogIndex = rf.getAbsoluteIndex(len(rf.log)) - 1
		//log.Printf("server %d 因为日志最大索引不够， 日志最大索引为%d, PrevLogIndex为 %d\n", rf.me, rf.getRelativeIndex(len(rf.log) - 1), PrevLogIndex)
		//log.Printf("sever %d 日志为 %v\n", rf.me, rf.log)
		return false, NewPrevLogIndex
	}

	//log.Printf("server %d 正在匹配日志, rf.lastIncludedIndex is %d, PrevLogIndex is %d\n", rf.me, rf.lastIncludedIndex, PrevLogIndex)

	if PrevLogIndex <= rf.lastIncludedIndex {
		return true, rf.lastIncludedIndex
	}

	if rf.log[rf.getRelativeIndex(PrevLogIndex)].Term != PrevLogTerm 	{  // 删除所有和leader不匹配的日志
		//log.Printf("server %d 因为日志任期不对， 日志任期为%d, PrevLogTerm为%d\n ", rf.me, rf.log[PrevLogIndex].Term, PrevLogTerm)
		for NewPrevLogIndex > 0 && rf.log[rf.getRelativeIndex(NewPrevLogIndex)].Term == rf.log[rf.getRelativeIndex(PrevLogIndex)].Term {
			NewPrevLogIndex--
		}
		return false, NewPrevLogIndex
	}
	return true, NewPrevLogIndex
}

func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	reply.Term = rf.currentTerm 
	reply.Success = false 

	//log.Printf("server %d 收到 leader %d 的心跳包, args.Term %d\n", rf.me, args.LeaderId, args.Term)

	if rf.currentTerm > args.Term {  // Reply false if term < currentTerm(5.1)
		//log.Printf("server %d 收到任期落后于自己的 leader %d \n", rf.me, args.LeaderId)
		return 
	}
	
	// leader 任期领先
	if rf.currentTerm < args.Term {
		rf.currentTerm = args.Term
		rf.persist()
	}
	
	rf.lastTime = time.Now()
	rf.status = "follower"

	ok, newPrevLogIndex := rf.isMatchPrevLog(args.PrevLogIndex, args.PrevLogTerm)
	if !ok { // 存在发送过来的日志不匹配问题
		//log.Printf("server %d 存在日志不匹配问题 leader %d, 不匹配日志位置为 %d\n", rf.me, args.LeaderId, args.PrevLogIndex)
		reply.NewPrevLogIndex = newPrevLogIndex
		reply.Success = false
	} else {
		// 反序列化数据
		restoredEntries := d_serialEntries(args.Entries)

		if restoredEntries != nil { // 发送过来的不是心跳包
			//log.Printf("节点 %d, 接收到的非空日志为 %v\n", rf.me, restoredEntries)
			if rf.getRelativeIndex(args.PrevLogIndex+1) > 0 {
				newLog := append(rf.log[:rf.getRelativeIndex(args.PrevLogIndex+1)], restoredEntries...)
				if rf.largestIndexMathWithLeader < rf.getAbsoluteIndex(len(newLog) - 1) {
					rf.log = newLog
					rf.largestIndexMathWithLeader = rf.getAbsoluteIndex(len(newLog) - 1)
					//log.Printf("成功同步日志来自leader %d , 节点 %d, 当前日志为 %v\n", args.LeaderId, rf.me, rf.log)
					rf.persist()
				} else {
					//log.Printf("不接受来自leader的日志, 节点 %d, leader %d 任期 %d, 节点当前日志长度: %d 新日志长度: %d \n", rf.me, args.LeaderId, args.Term, len(rf.log), len(newLog))
					// //log.Printf("不接受来自leader的日志, 节点 %d, leader %d 任期 %d, 节点当前日志长度: %d 新日志长度: %d \n当前日志: %v\n新日志 %v\n", rf.me, args.LeaderId, args.Term, len(rf.log), len(newLog), rf.log, newLog)
				}
			}
		} 

		if min(args.LeaderCommit, args.PrevLogIndex) > rf.commitIndex {
			//log.Printf("开始应用日志, 节点 %d, 已提交的索引为%d, 已应用的索引为 %d rf.lastIncludedIndex %d, args.LeaderCommit为 %d, args.PrevLogIndex为 %d\n", 
			//           rf.me, rf.commitIndex, rf.lastApplied, rf.lastIncludedIndex, args.LeaderCommit, args.PrevLogIndex)
			newIndex := min(args.LeaderCommit, args.PrevLogIndex)
			go rf.applyNewCommand(newIndex)
		}
		reply.Success = true
	}
}

func (rf *Raft) sendAppendEntries(server int, args *AppendEntriesArgs, reply *AppendEntriesReply) bool {
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
	return ok
}

//
// the service using Raft (e.g. a k/v server) wants to start
// agreement on the next command to be appended to Raft's log. if this
// server isn't the leader, returns false. otherwise start the
// agreement and return immediately. there is no guarantee that this
// command will ever be committed to the Raft log, since the leader
// may fail or lose an election. even if the Raft instance has been killed,
// this function should return gracefully.
//
// the first return value is the index that the command will appear at
// if it's ever committed. the second return value is the current
// term. the third return value is true if this server believes it is
// the leader.
//
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	index := -1
	term := -1
	isLeader := false

	// Your code here (2B).
	rf.mu.Lock()

	if rf.killed() || rf.status != "leader" {
		rf.mu.Unlock()
		return index, term, isLeader
	}

	newEntry := Entry{
		Term: rf.currentTerm,
		Command: command,
	}
	rf.log = append(rf.log, newEntry)
	term = rf.currentTerm
	index = rf.getAbsoluteIndex(len(rf.log) - 1)
	isLeader = true
	//log.Printf("server id %d, status: %s, 任期 %d, 追加新的命令 %v 及其索引 %d, 追加新命令后的日志为 %v\n\n",rf.me, rf.status, term, command, index, rf.log)
	rf.persist()
	rf.mu.Unlock()
	
	go rf.replicatedEntries()

	return index, term, isLeader
}

//
// the tester doesn't halt goroutines created by Raft after each test,
// but it does call the Kill() method. your code can use killed() to
// check whether Kill() has been called. the use of atomic avoids the
// need for a lock.
//
// the issue is that long-running goroutines use memory and may chew
// up CPU time, perhaps causing later tests to fail and generating
// confusing debug output. any goroutine with a long-running loop
// should call killed() to check whether it should stop.
//
func (rf *Raft) Kill() {
	atomic.StoreInt32(&rf.dead, 1)
	// Your code here, if desired.
}

func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
}

// The ticker go routine starts a new election if this peer hasn't received
// heartsbeats recently.
func (rf *Raft) ticker() {
	for rf.killed() == false {

		// Your code here to check if a leader election should
		// be started and to randomize sleeping time using
		// time.Sleep().
		<- rf.startElection

		rf.mu.Lock()
		if rf.status != "candidate"	{
			rf.mu.Unlock()
			continue
		}

		rf.currentTerm++
		rf.votedFor = rf.me
		rf.votedTerm = rf.currentTerm
		rf.persist()
		rf.lastTime = time.Now()
		rf.electionTime = getRandomizedElecionTime()

		electionTime := rf.electionTime
		total := len(rf.peers)
		term := rf.currentTerm

		LastLogTerm := rf.log[len(rf.log) - 1].Term
		LastLogIndex := rf.getAbsoluteIndex(len(rf.log) - 1)
		//log.Printf("server id %d, status: %s , 任期 %d, 开始选举, 日志为%v\n",rf.me, rf.status, rf.currentTerm, rf.log)
		rf.mu.Unlock()

		// //log.Printf("server id %d, status: %s 选举前给自己投了一票, 已完成%d \n",rf.me, rf.status, finished)
		
		args := RequestVoteArgs{
				Term: term,
				CandidateId: rf.me,
				LastLogTerm: LastLogTerm,
				LastLogIndex: LastLogIndex,
			}
		majority := total / 2 + 1
		count := 1
		finished := 1
		cond := sync.NewCond(&rf.mu)

		go func(electionTime time.Duration, term int, cond *sync.Cond) {
			time.Sleep(electionTime)
			if rf.atomicReadStatus() == "candidate" {
				//log.Printf("sever %d 在任期 %d 选举失败, 重新选举了\n", rf.me, term)
				cond.Broadcast()
				rf.startElection <- true  // 唤起选举线程
			}
		}(electionTime, term, cond)

		// //log.Printf("server id %d, status: %s 选举中, 已完成%d \n",rf.me, rf.status, finished)
		for i := 0; i < total; i++ {
			if i != rf.me {
				go func(server int) {					
					rf.mu.Lock()
					if rf.status != "candidate" {   // 以candidate状态发起投票
						cond.Broadcast()
						rf.mu.Unlock()
						return
					}
					rf.mu.Unlock()

					reply := RequestVoteReply{}
					if rf.sendRequestVote(server, &args, &reply) {
						rf.mu.Lock()
						if rf.status != "candidate" {   // 以candidate状态处理请求
							cond.Broadcast()
							rf.mu.Unlock()
							return
						}

						if reply.VoteGranted {
							//log.Printf("server id %d, status: %s 选举中, 任期%d, 得到%d的投票\n",rf.me, rf.status, rf.currentTerm, server)
							count++;
						} else {
							if reply.Term > rf.currentTerm {	// 变为follower, 但不重置计时器(成功接收leader消息，或者投票给candidate，成为candidate开启选举 重置计时器, )
								rf.currentTerm = reply.Term
								rf.persist()
								// rf.lastTime = time.Now()
								rf.status = "follower"
							} 
						}
						rf.mu.Unlock()
					}

					rf.mu.Lock()
					// //log.Printf("server id %d, status: %s 选举中, 任期: %d , %d 回复了\n", rf.me, rf.status, rf.currentTerm, server)
					finished++
					// //log.Printf("server id %d, status: %s, 任期: %d 选举中, 以获得 %d 票， 已完成 %d \n",rf.me, rf.status, rf.currentTerm, count, finished)
					cond.Broadcast()
					rf.mu.Unlock()
				}(i)
			}
		}

		rf.mu.Lock()
		for rf.status == "candidate" && count < majority && finished != total &&  // 满足条件一直阻塞
			time.Now().Sub(rf.lastTime) <= rf.electionTime { 
			// **关键代码**: 检测rpc超时导致的选举失败, 否则此线程将发生阻塞，无法进入下一次选举
			cond.Wait()
		}

		// 选举成功
		if rf.status == "candidate" && count >= majority  { 
			rf.becomeLeaderInit()
			go func() {
				rf.startSendHeartBeats <- true  // 唤起心跳线程
			}()
		}

		rf.mu.Unlock()
	}
}

func (rf *Raft) becomeLeaderInit() {
	rf.status = "leader"
	rf.nextIndex = make([]int, len(rf.peers))
	rf.matchIndex = make([]int, len(rf.peers))
	////log.Printf("选举成功，初始化 server id %d, status: %s, 任期: %d len(rf.log) %d \n",rf.me, rf.status, rf.currentTerm, len(rf.log))
	for i := range rf.nextIndex {
		rf.nextIndex[i] = rf.getAbsoluteIndex(len(rf.log))
		rf.matchIndex[i] = 0
	}
}

func (rf *Raft) checkState() {
	for rf.killed() == false {	
		rf.mu.Lock()
		if rf.status == "follower" && time.Now().Sub(rf.lastTime) > rf.electionTime {
			//log.Printf("server id %d, status: %s, 任期: %d, 选举时间为:%v rf.electionTime, 超时时间 %v\n",rf.me, rf.status, rf.currentTerm, rf.electionTime, time.Now().Sub(rf.lastTime))
			rf.status = "candidate"
			go func() {
				rf.startElection <- true  // 唤起选举线程
			}()
		}
		sleeptTime := rf.electionTime / 2;
		// //log.Printf("server %d status %s 任期 %d\n", rf.me, rf.status, rf.currentTerm)
		rf.mu.Unlock()
		time.Sleep(sleeptTime)
	}
}

func (rf *Raft) atomicReadStatus() string{
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.status
}

func (rf *Raft) replicatedEntries () { // 客户端发一条日志过来，leader开始复制给其他节点
	rf.mu.Lock()
	total := len(rf.peers)
	rf.mu.Unlock()

	for i := 0; i < total; i++ {
		if i != rf.me {
			go rf.replicatedEntriesToOne(i)
			go rf.sendInstallSnapshotToOne(i)
		}
	}
}

func (rf *Raft) applyNewCommand(newIndex int) {

	rf.mu.Lock()
	LastIndex := rf.getAbsoluteIndex(len(rf.log)-1)
	rf.lastApplied = max(rf.lastApplied, rf.lastIncludedIndex) // 防止崩溃后重启时，rf.lastApplied = 0 < rf.lastIncludedIndex
	for i := rf.lastApplied + 1; i <= min(newIndex, LastIndex); i++ {
		applyMsg := ApplyMsg{
			CommandValid: true,
			Command: rf.log[rf.getRelativeIndex(i)].Command,
			CommandIndex: i,
		}
		rf.applyCh <- applyMsg 
	}
	rf.lastApplied = newIndex
	rf.commitIndex = newIndex
	//log.Printf("server %d, 应用的最大命令索引为: %d, status %s\n 已应用的日志为%v", rf.me, rf.lastApplied, rf.status, rf.log)

	rf.mu.Unlock()
}

func (rf *Raft) sendHeartBeats() { // 发送心跳包，entries为空，且只发送一次不管是否成功
	for rf.killed() == false {
		<- rf.startSendHeartBeats
		for rf.killed() == false && rf.atomicReadStatus() == "leader" {
			rf.mu.Lock()
			total := len(rf.peers)
			rf.mu.Unlock()

			for i := 0; i < total; i++ {
				if i != rf.me {
					go rf.sendHeartBeatToOne(i)
					go rf.sendInstallSnapshotToOne(i)
				}
			}			
			time.Sleep(sendHeartbeatTime)
		}
	}
}


func (rf *Raft) sendHeartBeatToOne(server int) {
	rf.mu.Lock()

	if rf.status != "leader" ||  // 以leader身份发送心跳
	   rf.nextIndex[server] <= rf.lastIncludedIndex {   // // 要发送的起始位置日志还被删除，无法发送心跳
		rf.mu.Unlock()			
		return
	}

	args := AppendEntriesArgs{
		Term: rf.currentTerm,
		LeaderId: rf.me,
		PrevLogIndex: rf.nextIndex[server] - 1,
		PrevLogTerm: rf.log[rf.getRelativeIndex(rf.nextIndex[server] - 1)].Term,
		LeaderCommit: rf.commitIndex,
	}
	//log.Printf("sever %d status %s 任期为 %d, 发送心跳包给 server %d, PrevLogIndex is %d LeaderCommit is %d, rf.commitIndex is %d\n", rf.me, rf.status, rf.currentTerm, server, args.PrevLogIndex, args.LeaderCommit, rf.commitIndex)
	rf.mu.Unlock()	

	reply := AppendEntriesReply{}
	if rf.sendAppendEntries(server, &args, &reply) { // 收到回复	
		rf.mu.Lock()
		
		if rf.status != "leader" {  // 以leader身份处理回复
			rf.mu.Unlock()
			return
		}

		if !reply.Success { // leader任期落后或者日志不匹配
			if reply.Term > rf.currentTerm {			// 变为follower
				rf.currentTerm = reply.Term
				rf.persist()
				rf.lastTime = time.Now()
				rf.status = "follower"
			} else {	// 日志不匹配, 调整至匹配位置
				rf.nextIndex[server] = reply.NewPrevLogIndex + 1
			}
		} else { 	// leader和节点日志匹配
			//log.Printf("leader %d sever %d, rf.matchIndex[server]: %d, rf.getAbsoluteIndex(len(rf.log): %d\n", rf.me, server, rf.matchIndex[server], rf.getAbsoluteIndex(len(rf.log)))
			if rf.nextIndex[server] < rf.getAbsoluteIndex(len(rf.log)) {  // 防止部分匹配，更新要发送给该节点日志
				go rf.replicatedEntriesToOne(server)
			}
		}

		rf.mu.Unlock()
	}
}

func (rf *Raft) sendInstallSnapshotToOne(server int) {

	rf.mu.Lock()
	if rf.status != "leader" ||  // 以leader身份发送快照
	   rf.nextIndex[server] > rf.lastIncludedIndex {   // // 要发送的起始位置日志还未被删除，无法需发送快照
		rf.mu.Unlock()			
		return
 	}

	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	v := rf.log[0].Command
	e.Encode(v)

	args :=InstallSnapshotArgs{
		Term: rf.currentTerm,
		LeaderId: rf.me,
		LastIncludedIndex: rf.lastIncludedIndex,
		LastIncludedTerm: rf.lastIncludedTerm,
		Snapshot: w.Bytes(),		
	}
	//log.Printf("调用安装快照, leader %d, server %d\n", rf.me, server)
	rf.mu.Unlock()

	reply := InstallSnapshotReply{}
	if rf.sendInstallSnapshot(server, &args, &reply) { // 收到回复	
		rf.mu.Lock()
		if rf.status != "leader" {  // 以leader身份处理回复
			rf.mu.Unlock()
			return
		}

		if reply.Term > rf.currentTerm {			// 任期落后，快照不被接受，变为follower
			rf.currentTerm = reply.Term
			rf.persist()
			// rf.lastTime = time.Now()
			rf.status = "follower"
		} else { 	// 快照被接受
			rf.nextIndex[server] = rf.getAbsoluteIndex(len(rf.log))  // 该follower此时和leader日志一致, 刷新要发送的条目索引
		}
		rf.mu.Unlock()
	}
}

// 通过统计与leader日志匹配的节点数来应用日志
func (rf *Raft) leaderApply() {
	if rf.status != "leader" {
		return;
	}

	flag := false
	newIndex := rf.commitIndex
	for i := rf.getAbsoluteIndex(len(rf.log) - 1); i > rf.commitIndex; i-- {
		others := 0;
		for j := 0; j < len(rf.peers); j++ {
			if j == rf.me 	{
				continue;
			}
			if rf.matchIndex[j] >= i {
				others++
			} 
			if others >= len(rf.peers) / 2 {
				flag = true;
				break;
			}
		}
		if flag {
			newIndex = i;
			break;
		}
	}

	if flag {
		rf.commitIndex = newIndex
		for i := rf.lastApplied + 1; i <= newIndex; i++ {
			applyMsg := ApplyMsg{
				CommandValid: true,
				Command: rf.log[rf.getRelativeIndex(i)].Command,
				CommandIndex: i,
			}
			rf.applyCh <- applyMsg 
		}
		rf.lastApplied = newIndex
		//log.Printf("server %d status %s, 应用的最大命令索引为: %d\n 已应用的日志为%v", rf.me, rf.status, rf.lastApplied, rf.log)
	}
}


func (rf *Raft) replicatedEntriesToOne(server int) {
	rf.mu.Lock()

	if rf.status != "leader"  || rf.nextIndex[server] <= rf.lastIncludedIndex{  // 以leader身份向节点复制日志, 且可发送日志到该节点
		rf.mu.Unlock()
		return
	}

	args := AppendEntriesArgs{
		Term: rf.currentTerm,
		LeaderId: rf.me,
		PrevLogIndex: rf.nextIndex[server] - 1,
		PrevLogTerm: rf.log[rf.getRelativeIndex(rf.nextIndex[server] - 1)].Term,
		Entries: serialEntries(rf.log[rf.getRelativeIndex(rf.nextIndex[server]): ]), // 转化为字节数组
	}

	sendLogLastIndex := rf.getAbsoluteIndex(len(rf.log)) - 1
	rf.mu.Unlock()

	reply := AppendEntriesReply{}
	if rf.sendAppendEntries(server, &args, &reply) { // 收到回复
		rf.mu.Lock()

		if rf.status != "leader" { // 以leader身份处理回复
			rf.mu.Unlock()
			return
		}

		if !reply.Success { 	// 发送日志不成功
			if reply.Term > rf.currentTerm {  // 变为follower
				rf.currentTerm = reply.Term
				rf.persist()
				rf.lastTime = time.Now()
				rf.status = "follower"
			} else {						 // 日志不匹配
				rf.nextIndex[server] = reply.NewPrevLogIndex + 1
			}
		} else {  // 发送日志成功
			rf.nextIndex[server] = rf.getAbsoluteIndex(len(rf.log))  // 该follower此时和leader日志一致, 刷新要发送的条目索引
			rf.matchIndex[server] = sendLogLastIndex // 记录最大的匹配的条目索引, 用于应用条目
			//log.Printf("server id %d, status: %s, 任期: %d, 成功复制日志到 %d\n",rf.me, rf.status, rf.currentTerm, server)
			rf.leaderApply()  // 尝试应用新的条目，避免边界问题
		}
		rf.mu.Unlock()
	}
}
//
// the service or tester wants to create a Raft server. the ports
// of all the Raft servers (including this one) are in peers[]. this
// server's port is peers[me]. all the servers' peers[] arrays
// have the same order. persister is a place for this server to
// save its persistent state, and also initially holds the most
// recent saved state, if any. applyCh is a channel on which the
// tester or service expects Raft to send ApplyMsg messages.
// Make() must return quickly, so it should start goroutines
// for any long-running work.
//
func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.me = me

	rand.Seed(time.Now().UnixNano())
	// Your initialization code here (2A, 2B, 2C).
	rf.status = "follower"
	rf.currentTerm = 0
	rf.votedTerm = 0
	rf.lastTime = time.Now()
	rf.electionTime = getRandomizedElecionTime()
	rf.commitIndex = 0
	rf.lastApplied = 0
	rf.applyCh = applyCh
	rf.newCommitIndex = make(chan int)
	rf.startSendHeartBeats = make(chan bool)
	rf.startElection = make(chan bool)
	rf.log = []Entry {
		Entry{Term: 0,},
	}
	
	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())
	
	//log.Printf("inital raft sever %d\n", rf.me)
	// start ticker goroutine to start elections
	go rf.ticker()
	go rf.checkState()
	go rf.sendHeartBeats()
	return rf
}


func (rf *Raft) getRelativeIndex(absoluteIndex int) int {
	return absoluteIndex - rf.lastIncludedIndex
}

func (rf *Raft) getAbsoluteIndex(relativeIndex int) int {
	return relativeIndex + rf.lastIncludedIndex
}

func min(a int, b int) int {
	if a > b {
		return b
	} else {
		return a
	}
}

func max(a int, b int) int {
	if a > b {
		return a
	} else {
		return b
	}
}

func getRandomizedElecionTime() time.Duration {
	return time.Duration(rand.Intn(right-left) + left) * time.Millisecond
}

func serialEntries(entries []Entry) []byte{
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(entries); err != nil {
		log.Fatalln("序列化错误:", err)
	}
	return buf.Bytes()
}

func d_serialEntries(receivedBytes []byte) []Entry{
	// 创建一个新的bytes.Buffer
	var buffer bytes.Buffer
	var restoredEntries []Entry
	if len(receivedBytes) == 0 { return  restoredEntries}

	// 将接收到的字节数组写入到bytes.Buffer
	_, err := buffer.Write(receivedBytes)
	if err != nil {
		log.Fatalln("写入错误:", err)
	}

	dec := gob.NewDecoder(&buffer)
	
	if err := dec.Decode(&restoredEntries); err != nil {
		log.Fatalln("反序列化错误:", err)
	}

	return restoredEntries
}
