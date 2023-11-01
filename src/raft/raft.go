package raft

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
	right = 900
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
	Snapshot      []byte
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
	persister *Persister          // Object to hold this peer's persisted state
	me        int                 // this peer's index into peers[]
	dead      int32               // set by Kill()

	// Your data here (2A, 2B, 2C).
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.

	status string // raft 服务器的状态：leader, follower, candidate
	currentTerm int
	votedFor int 
	votedTerm int
	log []Entry
	
	lastTime time.Time
	electionTime time.Duration

	commitIndex int 
	lastApplied int 

	nextIndex []int
	matchIndex []int
	leaderId int

	newCommitIndex chan int
	applyCh chan ApplyMsg
	startSendHeartBeats chan bool
	startElection chan bool
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
	if d.Decode(&currentTerm) != nil ||
	   d.Decode(&votedFor) != nil || 
	   d.Decode(&votedTerm) != nil ||
	   d.Decode(&restortedLog) != nil {
		log.Fatalln("Decode err")
	} else {
		rf.currentTerm = currentTerm
		rf.votedFor = votedFor
		rf.votedTerm = votedTerm
		rf.log = restortedLog
	}
}


//
// A service wants to switch to snapshot.  Only do so if Raft hasn't
// have more recent info since it communicate the snapshot on applyCh.
//
func (rf *Raft) CondInstallSnapshot(lastIncludedTerm int, lastIncludedIndex int, snapshot []byte) bool {

	// Your code here (2D).

	return true
}

// the service says it has created a snapshot that has
// all info up to and including index. this means the
// service no longer needs the log through (and including)
// that index. Raft should now trim its log as much as possible.
func (rf *Raft) Snapshot(index int, snapshot []byte) {
	// Your code here (2D).

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
	log.Printf("server %d status %s 任期 %d, 收到 server %d 任期 %d 的投票请求\n", rf.me, rf.status, rf.currentTerm, args.CandidateId, args.Term)
	if args.Term < rf.currentTerm { // candidate 任期落后，不可能投票
		reply.VoteGranted = false
	} else if args.Term >= rf.currentTerm { // candidate 任期不落后，可能投票
		rf.currentTerm = args.Term
		if rf.isAdvanceThanCandidate(args.LastLogTerm, args.LastLogIndex) {	// candidate日志落后, 不投票
			reply.VoteGranted = false
		}	else {			// candidate日志不落后, 可能投票
			if rf.votedTerm < args.Term { // 此server在这轮任期中未投过票，投票
				reply.VoteGranted = true
				rf.votedTerm = args.Term
				rf.votedFor = args.CandidateId
				rf.lastTime = time.Now()
				rf.status = "follower"
			} else {
				reply.VoteGranted = false // 已投票给其他人
			}
		}
		rf.persist()
	}
}

func (rf *Raft) isAdvanceThanCandidate(LastLogTerm int, LastLogIndex int) bool {
	tail := len(rf.log) - 1
	if rf.log[tail].Term > LastLogTerm { return true }
	if rf.log[tail].Term < LastLogTerm { return false }
	return tail > LastLogIndex
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
	if PrevLogIndex >= len(rf.log) { 
		NewPrevLogIndex = len(rf.log) - 1
		log.Printf("server %d 因为日志最大索引不够， 日志最大索引为%d, PrevLogIndex为 %d\n", rf.me, len(rf.log) - 1, PrevLogIndex)
		log.Printf("sever %d 日志为 %v\n", rf.me, rf.log)
		return false, NewPrevLogIndex
	}
	if rf.log[PrevLogIndex].Term != PrevLogTerm 	{  // 删除所有和leader不匹配的日志
		log.Printf("server %d 因为日志任期不对， 日志任期为%d, PrevLogTerm为%d\n ", rf.me, rf.log[PrevLogIndex].Term, PrevLogTerm)
		for NewPrevLogIndex > 0 && rf.log[NewPrevLogIndex].Term == rf.log[PrevLogIndex].Term {
			NewPrevLogIndex--
		}
		return false, NewPrevLogIndex
	}
	return true, NewPrevLogIndex
}

func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	reply.Term = rf.currentTerm  // 
	reply.Success = false 
	log.Printf("server %d 收到 leader %d 的心跳包, args.Term %d\n", rf.me, args.LeaderId, args.Term)

	if rf.currentTerm > args.Term {  // Reply false if term < currentTerm(5.1)
		log.Printf("server %d 收到任期落后于自己的 leader %d \n", rf.me, args.LeaderId)
		return 
	}
	
	// leader 任期领先 或和server同一任期
	if rf.currentTerm < args.Term {
		rf.currentTerm = args.Term
		rf.persist()
	}
	
	rf.lastTime = time.Now()
	rf.status = "follower"

	ok, newPrevLogIndex := rf.isMatchPrevLog(args.PrevLogIndex, args.PrevLogTerm)
	if !ok { // 存在发送过来的日志不匹配问题
		log.Printf("server %d 存在日志不匹配问题 leader %d, 不匹配日志位置为 %d\n", rf.me, args.LeaderId, args.PrevLogIndex)
		reply.NewPrevLogIndex = newPrevLogIndex
		reply.Success = false
	} else {
		// 反序列化数据
		restoredEntries := d_serialEntries(args.Entries)

		// log.Printf("节点 %d, 接收到的日志为 %v\n", rf.me, newEntries)
		if restoredEntries != nil { // 发送过来的不是心跳包
			
			log.Printf("节点 %d, 接收到的非空日志为 %v\n", rf.me, restoredEntries)
			// newLog = append(rf.log[:args.PrevLogIndex+1], restoredEntries...)

			rf.log = append(rf.log[:args.PrevLogIndex+1], restoredEntries...)
			
			log.Printf("成功同步日志, 节点 %d, 当前日志为 %v\n", rf.me, rf.log)

			rf.persist()
		} 


		if min(args.LeaderCommit, args.PrevLogIndex) > rf.commitIndex {
			log.Printf("开始应用日志, 节点 %d, 已应用的索引为%d, args.LeaderCommit为 %d, args.PrevLogIndex为 %d\n", rf.me, rf.commitIndex, args.LeaderCommit, args.PrevLogIndex)
			rf.commitIndex = min(args.LeaderCommit, args.PrevLogIndex)
			newIndex := rf.commitIndex
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
	index = len(rf.log) - 1
	isLeader = true
	log.Printf("server id %d, status: %s, 任期 %d, 追加新的命令 %v 及其索引 %d, 追加新命令后的日志为 %v\n\n",rf.me, rf.status, term, command, index, rf.log)
	// log.Printf("server id %d, status: %s, 任期 %d, 追加新命令后的日志为 %v\n",rf.me, rf.status, term, rf.log)
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
		rf.currentTerm++
		rf.votedFor = rf.me
		rf.votedTerm = rf.currentTerm
		rf.persist()
		rf.lastTime = time.Now()
		rf.electionTime = getRandomizedElecionTime()
		electionTime := rf.electionTime

		total := len(rf.peers)
		term := rf.currentTerm
		LastLogIndex := len(rf.log) - 1
		LastLogTerm := rf.log[LastLogIndex].Term
		log.Printf("server id %d, status: %s , 任期 %d, 开始选举, 日志为%v\n",rf.me, rf.status, rf.currentTerm, rf.log)
		rf.mu.Unlock()

		// log.Printf("server id %d, status: %s 选举前给自己投了一票, 已完成%d \n",rf.me, rf.status, finished)
		
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
				log.Printf("sever %d 在任期 %d 选举失败, 重新选举了\n", rf.me, term)
				cond.Broadcast()
				rf.startElection <- true  // 唤起选举线程
			}
		}(electionTime, term, cond)

		// log.Printf("server id %d, status: %s 选举中, 已完成%d \n",rf.me, rf.status, finished)
		for i := 0; i < total; i++ {
			if i != rf.me {
				go func(server int) {
					reply := RequestVoteReply{}
					rf.mu.Lock()
					log.Printf("server id %d, status: %s 选举中, 任期%d , 向%d申请投票 \n",rf.me, rf.status, rf.currentTerm, server)
					rf.mu.Unlock()

					if rf.sendRequestVote(server, &args, &reply) {
						rf.mu.Lock()
						
						if reply.Term > rf.currentTerm {			// 变为follower
							rf.currentTerm = reply.Term
							rf.persist()
							rf.lastTime = time.Now()
							rf.status = "follower"
						} else {
							if reply.VoteGranted {
								log.Printf("server id %d, status: %s 选举中, 任期%d, 得到%d的投票\n",rf.me, rf.status, rf.currentTerm, server)
								count++;
							}
						}
						rf.mu.Unlock()
					}

					rf.mu.Lock()
					log.Printf("server id %d, status: %s 选举中, 任期: %d , %d 回复了\n", rf.me, rf.status, rf.currentTerm, server)
					finished++
					log.Printf("server id %d, status: %s, 任期: %d 选举中, 以获得 %d 票， 已完成 %d \n",rf.me, rf.status, rf.currentTerm, count, finished)
					cond.Broadcast()
					rf.mu.Unlock()
				}(i)
			}
		}

		rf.mu.Lock()
		for rf.status == "candidate" && count < majority && finished != total &&  // 满足条件一直阻塞
			time.Now().Sub(rf.lastTime) <= rf.electionTime { // **检测rpc超时导致的选举失败, 否则此线程将发生阻塞，无法进入下一次选举
			cond.Wait()
		}

		if rf.status == "candidate" && count >= majority  {  // 选举成功
			rf.status = "leader"
			rf.nextIndex = make([]int, len(rf.peers))
			rf.matchIndex = make([]int, len(rf.peers))
			log.Printf("选举成功，初始化 server id %d, status: %s, 任期: %d len(rf.log) %d \n",rf.me, rf.status, rf.currentTerm, len(rf.log))
			for i := range rf.nextIndex {
				rf.nextIndex[i] = len(rf.log)
				rf.matchIndex[i] = 0
			}

			go func() {
				rf.startSendHeartBeats <- true  // 唤起心跳线程
			}()
		}

		rf.mu.Unlock()
	}
}

func (rf *Raft) checkState() {
	for rf.killed() == false {	
		rf.mu.Lock()
		if rf.status == "follower" && time.Now().Sub(rf.lastTime) > rf.electionTime {
			log.Printf("server id %d, status: %s, 任期: %d, 选举时间为:%v rf.electionTime, 超时时间 %v\n",rf.me, rf.status, rf.currentTerm, rf.electionTime, time.Now().Sub(rf.lastTime))
			rf.status = "candidate"
			go func() {
				rf.startElection <- true  // 唤起选举线程
			}()
		}
		sleeptTime := rf.electionTime / 2;
		log.Printf("server %d status %s 任期 %d\n", rf.me, rf.status, rf.currentTerm)
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
	// nowLogLength := len(rf.log)    // 每次开始复制日志时的长度
	// log.Printf("server id %d, status: %s, 任期: %d, 开始复制日志\n",rf.me, rf.status, rf.currentTerm)
	rf.mu.Unlock()
	majority := total / 2 + 1
	finished := 1
	cond := sync.NewCond(&rf.mu)

	for i := 0; i < total; i++ {
		if i != rf.me {
			go func(server int) {
				reply := AppendEntriesReply{}
				flag := false
				// 一直重发直到成功复制匹配之后的日志条目, 但raft 服务器被杀死之后不再重发, 避免占用cpu
				// 测评代码并非真正的清理raft服务器, 而是将标识符置为1
				for !rf.killed() && !flag { 
					rf.mu.Lock()

					if rf.status != "leader" {
						rf.mu.Unlock()
						cond.Broadcast() 
						return
					}

					args := AppendEntriesArgs{
						Term: rf.currentTerm,
						LeaderId: rf.me,
						PrevLogIndex: rf.nextIndex[server] - 1,
						PrevLogTerm: rf.log[rf.nextIndex[server] - 1].Term,
						Entries: serialEntries(rf.log[rf.nextIndex[server]: ]), // 转化为字节数组
					}
					nowLogLength := len(rf.log)
					rf.mu.Unlock()


					if rf.sendAppendEntries(server, &args, &reply) { // 收到回复
						rf.mu.Lock()

						// log.Printf("server id %d, status: %s, 任期: %d, 发送给 %d 的日志为 %v\n",rf.me, rf.status, rf.currentTerm, server, newEntries)

						if !reply.Success { 	// 发送日志不成功
							if reply.Term > rf.currentTerm {  // 变为follower
								rf.currentTerm = reply.Term
								rf.persist()
								rf.lastTime = time.Now()
								rf.status = "follower"
								flag = true
								cond.Broadcast()
							} else {						 // 日志不匹配
								// if rf.nextIndex[server] > 1 {
								// 	rf.nextIndex[server]--
								// }
								rf.nextIndex[server] = reply.NewPrevLogIndex + 1
							}
						} else {  // 发送日志成功
							flag = true
							rf.nextIndex[server] = len(rf.log)  // 该follower此时和leader日志一致, 刷新要发送的条目索引
							rf.matchIndex[server] = nowLogLength - 1 // 记录最大的匹配的条目索引, 用于应用条目

							log.Printf("server id %d, status: %s, 任期: %d, 成功复制日志到 %d\n",rf.me, rf.status, rf.currentTerm, server)
						}
						rf.mu.Unlock()
					}
				}
				
				rf.mu.Lock()                        
				//收到成功回复退出
				finished++
				cond.Broadcast()                    
				rf.mu.Unlock()
			}(i)
		}
	}

	// flag := false
	rf.mu.Lock()
	for rf.status == "leader" && finished < majority { // 满足所有条件一直阻塞
		cond.Wait()
	}
	
	// status := rf.status;
	// replicatedSuccess := (finished >= majority)
	rf.mu.Unlock()

	// if rf.killed() == false && status == "leader" && replicatedSuccess { 
	// 	rf.mu.Lock()
	// 	log.Printf("server id %d, status: %s, 任期 %d, leader成功将日志复制到大多数是为 %v\n",rf.me, rf.status, rf.currentTerm, rf.log)
	// 	if nowLogLength - 1 > rf.commitIndex {
	// 		rf.commitIndex = nowLogLength - 1
	// 		// newIndex := rf.commitIndex
	// 		go rf.applyNewCommand(nowLogLength - 1)  // 执行新提交的条目
	// 	}
	// 	rf.mu.Unlock()
	// }
}

func (rf *Raft) applyNewCommand(newIndex int) {

	rf.mu.Lock()
	for i := rf.lastApplied + 1; i <= newIndex; i++ {
		// log.Printf("log index %d\n", i)
		applyMsg := ApplyMsg{
			CommandValid: true,
			Command: rf.log[i].Command,
			CommandIndex: i,
		}
		// log.Printf("server %d 执行新的committed命令: %v 命令类型%T\n", rf.me, applyMsg, applyMsg.Command)
		rf.applyCh <- applyMsg 
	}
	rf.lastApplied = newIndex
	log.Printf("server %d status %s, 应用的最大命令索引为: %d\n 已应用的日志为%v", rf.me, rf.status, rf.lastApplied, rf.log)

	rf.mu.Unlock()
}

func (rf *Raft) sendHeartBeats() { // 发送心跳包，entries为空，且只发送一次不管是否成功
	for rf.killed() == false {
		<- rf.startSendHeartBeats
		for rf.atomicReadStatus() == "leader" {
			// 加锁保护
			rf.mu.Lock()
			total := len(rf.peers)
			rf.mu.Unlock()

			for i := 0; i < total; i++ {
				if i != rf.me {
					go func(server int) {
						reply := AppendEntriesReply{}

						rf.mu.Lock()
						if rf.status != "leader" {  // 防止出现leader任期落后当成日志不匹配情况
							rf.mu.Unlock()
							return
						}
						args := AppendEntriesArgs{
							Term: rf.currentTerm,
							LeaderId: rf.me,
							PrevLogIndex: rf.nextIndex[server] - 1,
							PrevLogTerm: rf.log[rf.nextIndex[server] - 1].Term,
							LeaderCommit: rf.commitIndex,
						}
						rf.mu.Unlock()

						if rf.sendAppendEntries(server, &args, &reply) { // 收到回复
							rf.mu.Lock()
							log.Printf("sever %d status %s 发送心跳包给 server %d, PrevLogIndex is %d LeaderCommit is %d, rf.commitIndex is %d\n", rf.me, rf.status, server, args.PrevLogIndex, args.LeaderCommit, rf.commitIndex)
							rf.mu.Unlock()

							if !reply.Success { // leader任期落后或者日志不匹配
								rf.mu.Lock()
								if reply.Term > rf.currentTerm {			// 变为follower
									rf.currentTerm = reply.Term
									rf.persist()
									rf.lastTime = time.Now()
									rf.status = "follower"
									rf.mu.Unlock()
								} else {
									rf.mu.Unlock()
									if rf.atomicReadStatus() == "leader" {
										go rf.replicatedEntries()
									}
								}
							}
						}
					}(i)
				}
			}

			rf.mu.Lock() 
			flag := false
			newIndex := rf.commitIndex
			if len(rf.log) - 1 > rf.commitIndex {
				for i := len(rf.log) - 1; i > rf.commitIndex; i-- {
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
			}
			if flag {
				rf.commitIndex = newIndex
			}
			rf.mu.Unlock() 

			if flag {
				go rf.applyNewCommand(newIndex)  // 执行新提交的条目
			}

			time.Sleep(sendHeartbeatTime)
		}
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
	
	log.Printf("inital raft sever %d\n", rf.me)
	// start ticker goroutine to start elections
	go rf.ticker()
	go rf.checkState()
	go rf.sendHeartBeats()

	return rf
}



func min(a int, b int) int {
	if a > b {
		return b
	} else {
		return a
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
