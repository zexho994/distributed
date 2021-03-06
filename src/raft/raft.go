package raft

//
// this is an outline of the API that raft must expose to
// the service (or tester). see comments below for
// each of these functions for more details.
//
// rf = Make(...)
//   create a new Raft server.
// rf.Start(command interface{}) (index, Term, isleader)
//   start agreement on a new log entry
// rf.GetState() (Term, isLeader)
//   ask a Raft for its current Term, and whether it thinks it is leader
// ApplyMsg
//   each time a new entry is committed to the log, each Raft Peer
//   should send an ApplyMsg to the service (or tester)
//   in the same server.
//

import (
	"bytes"
	"log"
	"math/rand"
	"sort"
	"sync"
	"time"
)
import "sync/atomic"
import "../labrpc"
import "../labgob"

// import "bytes"
// import "../labgob"

// ApplyMsg
// as each Raft Peer becomes aware that successive log entries are
// committed, the Peer should send an ApplyMsg to the service (or
// tester) on the same server, via the applyCh passed to Make(). set
// CommandValid to true to indicate that the ApplyMsg contains a newly
// committed log entry.
//
// in Lab 3 you'll want to send other kinds of messages (e.g.,
// snapshots) on the applyCh; at that point you can add fields to
// ApplyMsg, but set CommandValid to false for these other uses.
//
type ApplyMsg struct {
	CommandValid bool
	Command      interface{}
	CommandIndex int
}

//
// A Go object implementing a single Raft Peer.
//
type Raft struct {
	mu        sync.Mutex          // Lock to protect shared access to this Peer's state
	peers     []*labrpc.ClientEnd // RPC end points of all peers
	persister *Persister          // Object to hold this Peer's persisted state
	me        int                 // this Peer's index into peers[]
	dead      int32               // set by Kill()

	// Your data here (2A, 2B, 2C).
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.

	// = = = = 服务器持久保存的数据 = = = =
	//任期号
	Term int32
	//获得选票的服务器
	VotedFor int
	//角色类型
	Role int32
	// 日志数据
	Log []LogEntry

	// = = = = 所有服务器都会变的部分 = = = =
	// 下一个选举超时时间
	NextVoteTimeout time.Time
	// 下一个心跳超时时间
	NextHeartTimeout time.Time
	// 已经提交的日志索引
	CommittedIndex int
	// 最后应用到状态机的日志索引
	LastAppliedIndex int

	// = = = = Leader服务器会变的部分(选举后初始化) = = = =
	//对于每⼀个服务器，需要发送给他的下⼀个⽇志条⽬的索引值（初始化为领导⼈最后索引值加⼀）
	NextIndex []int
	//对于每⼀个服务器，已经复制给他的⽇志的最⾼索引值
	MatchIndex []int

	Apply chan ApplyMsg
}

// 日志对象
type LogEntry struct {
	Index int
	// 收到该日志时的term
	Term int32
	// 数据实体
	Command interface{}
}

func (rf *Raft) appendLog(offset int, nlog []LogEntry) {
	for _, loge := range nlog {
		//DPrintf("[raft-%v %v %v] 追加日志, idx = %v, log = %v \n", rf.me, rf.getRole(), rf.Term, idx+offset, loge)
		rf.Log = append(rf.Log, loge)
	}
}

func (rf *Raft) checkTimeout() bool {
	return rf.NextVoteTimeout.Before(time.Now())
}

func (rf *Raft) isLeader() bool {
	return rf.Role == Leader
}

func (rf *Raft) isFollower() bool {
	return rf.Role == Follower
}

func (rf *Raft) isCandidate() bool {
	return rf.Role == Candidate
}

func (rf *Raft) updateVoteTime() {
	rt := time.Duration(rand.Uint32()%200) + 200
	rf.NextVoteTimeout = time.Now().Add(rt * time.Millisecond)
}

func (rf *Raft) updateHeartTime() {
	rt := time.Duration(rand.Uint32()%150) + 150
	rf.NextHeartTimeout = time.Now().Add(rt * time.Millisecond)
}

func (rf *Raft) changeToLeader() {
	DPrintf("[raft-%v %v %v] 修改状态 => Leader .\n", rf.me, rf.getRole(), rf.Term)
	rf.Role = Leader
	rf.NextIndex = make([]int, len(rf.peers))
	rf.MatchIndex = make([]int, len(rf.peers))
	for i := 0; i < len(rf.peers); i++ {
		rf.MatchIndex[i] = -1
		rf.NextIndex[i] = len(rf.Log)
	}
}

func (rf *Raft) changeToCandidate() {
	DPrintf("[raft-%v %v %v] 修改状态 => Candidate.\n", rf.me, rf.getRole(), rf.Term)
	rf.Role = Candidate
}

// 变成follower,重置投票情况
func (rf *Raft) changeToFollower(term int32) {
	DPrintf("[raft-%v %v %v] 修改状态 => Follow. biggerTerm = %v \n", rf.me, rf.getRole(), rf.Term, term)
	rf.Role = Follower
	rf.Term = term
	rf.VotedFor = -1
	rf.updateHeartTime()
	rf.updateVoteTime()
}

const Leader int32 = 1
const Follower int32 = 2
const Candidate int32 = 3

func (rf *Raft) getRole() string {
	if rf.Role == Leader {
		return "Leader"
	}
	if rf.Role == Follower {
		return "Follower"
	}
	if rf.Role == Candidate {
		return "Candidate"
	}
	log.Panicf("get Role error, r = %v \n", rf.Role)
	return "Unknown"
}

// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {
	var term int
	var isleader bool
	// Your Code here (2A).
	rf.mu.Lock()
	defer rf.mu.Unlock()
	term = int(rf.Term)
	isleader = rf.isLeader()
	//DPrintf("[raft-%v %v %v] get state = %v\n", rf.me, getRole(rf.Role), term, isleader)
	return term, isleader
}

//
// save Raft's persistent state to stable storage,
// where it can later be retrieved after a crash and restart.
// see paper's Figure 2 for a description of what should be persistent.
//
func (rf *Raft) persist() {
	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	e.Encode(rf.Term)
	e.Encode(rf.LastAppliedIndex)
	e.Encode(rf.CommittedIndex)
	e.Encode(rf.Log)
	e.Encode(rf.VotedFor)
	data := w.Bytes()
	rf.persister.SaveRaftState(data)
	//DPrintf("[raft-%v %v %v] 数据持久化完成 \n", rf.me, rf.getRole(), rf.Term)
}

//
// restore previously persisted state.
//
func (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}

	DPrintf("[raft-%v %v %v] == 读取持久化数据 == \n", rf.me, rf.getRole(), rf.Term)
	r := bytes.NewBuffer(data)
	d := labgob.NewDecoder(r)
	var term int32
	var logs []LogEntry
	var appIdx, commitIdx, votedFor int
	if d.Decode(&term) != nil ||
		d.Decode(&appIdx) != nil ||
		d.Decode(&commitIdx) != nil ||
		d.Decode(&logs) != nil ||
		d.Decode(&votedFor) != nil {
		log.Fatalln("read persist fail")
		return
	}
	rf.Term = term
	rf.LastAppliedIndex = appIdx
	rf.CommittedIndex = commitIdx
	rf.Log = logs
	rf.VotedFor = votedFor
	DPrintf("[raft-%v %v %v] term = %v. \n", rf.me, rf.getRole(), rf.Term, rf.Term)
	DPrintf("[raft-%v %v %v] lastAppliedIndex = %v. \n", rf.me, rf.getRole(), rf.Term, rf.LastAppliedIndex)
	DPrintf("[raft-%v %v %v] committedIndex = %v. \n", rf.me, rf.getRole(), rf.Term, rf.CommittedIndex)
	DPrintf("[raft-%v %v %v] logs.len = %v. \n", rf.me, rf.getRole(), rf.Term, len(rf.Log))
}

//
// example RequestVote RPC arguments structure.
// field names must start with capital letters!
//
type RequestVoteArgs struct {
	// Your data here (2A, 2B).
	// 竞选人id
	Peer int
	// 竞选人term
	Term int32
	// the last log index of candidate
	LastLogIndex int
	// the last term of candidate
	LastLogTerm int32
}

//
// example RequestVote RPC reply structure.
// field names must start with capital letters!
//
type RequestVoteReply struct {
	// Your data here (2A).
	Peer int
	Term int32
	// -1初始化，1投票通过, 2投票未通过
	VoteGranted VoteResult
}

type VoteResult int

const Init = -1
const Success = 1
const TermTooSmall = 2
const LogTooSmall = 3
const HaveVoted = 4
const HaveLeader = 5

type AppendEntriesArgs struct {
	// leader id
	Peer int
	// leader term
	Term int32
	// previous index of this log
	PrevLogIndex int
	// previous term of PrevLogIndex
	PrevLogTerm int32
	// logs
	Entries []LogEntry
	// the committed log index of leader
	LeaderCommitted int
}

type AppendEntriesReply struct {
	// replier term
	Term int32
	// rpc result
	Result bool
}

//
// example RequestVote RPC handler.
//
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	// Your Code here (2A, 2B).
	rf.mu.Lock()
	defer rf.mu.Unlock()

	DPrintf("[raft-%v %v %v %v %v] 处理%v的投票RPC. T = %v. logIdx = %v. logTerm = %v. \n", rf.me, rf.getRole(), rf.Term, rf.maxLogIdx(), rf.maxLogTerm(), args.Peer, args.Term, args.LastLogIndex, args.LastLogTerm)
	reply.Term = rf.Term

	if args.Term > rf.Term {
		rf.changeToFollower(args.Term)
	}

	if !rf.checkVoteTeam(*args) {
		reply.VoteGranted = TermTooSmall
		return
	} else if !rf.checkVoteLog(*args) {
		reply.VoteGranted = LogTooSmall
		return
	}

	if rf.VotedFor == -1 || rf.VotedFor == args.Peer {
		reply.VoteGranted = Success
		rf.VotedFor = args.Peer
		rf.updateHeartTime()
		rf.updateVoteTime()
		DPrintf("[raft-%v %v %v] 给候选人%v投票 \n", rf.me, rf.getRole(), reply.Term, args.Peer)
	} else {
		reply.VoteGranted = HaveVoted
	}
}

func (rf *Raft) checkVoteTeam(args RequestVoteArgs) bool {
	// 候选人term太小，反对
	if args.Term < rf.Term {
		DPrintf("[raft-%v %v %v] 候选人T太小，拒绝投票 \n", rf.me, rf.getRole(), rf.Term)
		return false
	}
	return true
}

func (rf *Raft) checkVoteLog(args RequestVoteArgs) bool {
	// 日志大小
	if args.LastLogTerm < rf.maxLogTerm() ||
		(args.LastLogTerm == rf.maxLogTerm() && args.LastLogIndex < rf.maxLogIdx()) {
		DPrintf("[raft-%v %v %v] 候选人日志落后，拒绝投票 \n", rf.me, rf.getRole(), rf.Term)
		rf.Term = maxInt32(rf.Term, args.Term)
		return false
	}

	return true
}

func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	DPrintf("[raft-%v %v %v] 收到%v心跳rpc: preIdx=%v, preTerm=%v, lcommit=%v, log=%v  \n", rf.me, rf.getRole(), rf.Term, args.Peer, args.PrevLogIndex, args.PrevLogTerm, args.LeaderCommitted, args.Entries)
	reply.Term, reply.Result = rf.Term, false

	// 检查 T
	if args.Term < rf.Term {
		DPrintf("[raft-%v %v %v] 抛弃过期rpc, T = %v  \n", rf.me, rf.getRole(), rf.Term, args.Term)
		return
	}

	if rf.isCandidate() {
		rf.changeToFollower(args.Term)
	} else {
		rf.updateHeartTime()
		rf.updateVoteTime()
	}

	// 检查日志是否落后
	if args.PrevLogIndex >= len(rf.Log) || (args.PrevLogIndex >= 0 && rf.Log[args.PrevLogIndex].Term != args.PrevLogTerm) {
		return
	}

	reply.Result = true
	if args.Entries != nil {
		if args.PrevLogIndex < len(rf.Log) {
			rf.Log = rf.Log[:args.PrevLogIndex+1]
		}
		rf.appendLog(args.PrevLogIndex+1, args.Entries)
	}

	// 更新提交位置
	if rf.CommittedIndex < args.LeaderCommitted {
		DPrintf("[raft-%v %v %v] 更新 commitIdx, from %v to %v \n", rf.me, rf.getRole(), rf.Term, rf.CommittedIndex, args.LeaderCommitted)
		rf.CommittedIndex = args.LeaderCommitted
		rf.applyMsg()
	}

	// 日志操作lab-2A不实现
	rf.persist()
}

func (rf *Raft) updateCommitIndex(idx int) {
	rf.CommittedIndex = idx
}

func maxInt(n1, n2 int) int {
	if n1 > n1 {
		return n1
	}
	return n2
}

func maxInt32(n1, n2 int32) int32 {
	if n1 > n2 {
		return n1
	}
	return n2
}

func min(n1, n2 int) int {
	if n1 < n2 {
		return n1
	}
	return n2
}

//
// example Code to send a RequestVote RPC to a server.
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
// within a checkTimeout interval, Call() returns true; otherwise
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

func (rf *Raft) sendAppendEntries(server int, args *AppendEntriesArgs, reply *AppendEntriesReply) bool {
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
	return ok
}

// Start
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
// Term. the third return value is true if this server believes it is
// the leader.
//
// @param int index of committed log
// @param int term
// @param bool is true if this server believers it is the leader
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	index := -1
	term := -1
	isLeader := true

	// Your Code here (2B).
	rf.mu.Lock()
	defer rf.mu.Unlock()
	isLeader = rf.isLeader()
	if isLeader {
		index, term = len(rf.Log)+1, int(rf.Term)
		//DPrintf("[raft-%v %v %v] 客户端请求，cmd = %v \n", rf.me, rf.getRole(), rf.Term, command)
		rf.Log = append(rf.Log, LogEntry{Index: index, Term: rf.Term, Command: command})
	}
	return index, term, isLeader
}

//
// the tester doesn't halt goroutines created by Raft after each test,
// but it does call the Kill() method. your Code can use killed() to
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
	// Your Code here, if desired.
}

func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
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
// params:
// peers is an array of network identifiers of the Raft peers
// me is the index of this Peer
//
func Make(peers []*labrpc.ClientEnd, me int, persister *Persister, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.me = me
	// Your initialization Code here (2A, 2B, 2C).
	rf.Term = 0
	rf.Role = Follower
	rf.VotedFor = -1
	// 设置下次心跳检查时间
	rf.updateVoteTime()
	rf.updateHeartTime()
	rf.LastAppliedIndex = -1
	rf.CommittedIndex = -1
	rf.NextIndex = make([]int, len(peers))
	//rf.MatchIndex = make([]int, len(peers))
	rf.Apply = applyCh
	// 维持状态的协程
	DPrintf("[raft-%v %v %v] Make raft { peers.len = %v } \n", rf.me, rf.getRole(), rf.Term, len(rf.peers))
	go rf.maintainStateLoop()
	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	return rf
}

func (rf *Raft) maintainStateLoop() {
	for !rf.killed() {
		rf.mu.Lock()
		if rf.isLeader() {
			rf.maintainsLeader()
		} else if rf.isFollower() {
			rf.maintainsFollower()
		} else if rf.isCandidate() {
			rf.maintainsCandidate()
		} else {
			log.Fatalf("[raft-%v %v]Role type error : %v \n", rf.me, rf.getRole(), rf.Role)
		}
	}
}

// leader
// step1 : send heartbeat to peers
func (rf *Raft) maintainsLeader() {
	for idx := range rf.peers {
		if idx == rf.me {
			continue
		}
		go func(from int, to int, term int32) {
			// args
			rf.mu.Lock()
			currMaxIdx := len(rf.Log) - 1
			args := AppendEntriesArgs{Peer: rf.me, Term: rf.Term}
			args.PrevLogIndex = rf.NextIndex[to] - 1
			args.LeaderCommitted = rf.CommittedIndex
			if args.PrevLogIndex < 0 || args.PrevLogIndex > currMaxIdx {
				args.PrevLogTerm = -1
			} else {
				args.PrevLogTerm = rf.Log[args.PrevLogIndex].Term
			}
			if args.PrevLogIndex < currMaxIdx {
				args.Entries = rf.Log[args.PrevLogIndex+1 : len(rf.Log)]
			}
			rf.mu.Unlock()

			// send rpc
			reply := AppendEntriesReply{}
			flag := rf.sendAppendEntries(to, &args, &reply)

			// process rpc reply
			rf.mu.Lock()
			defer rf.mu.Unlock()
			if rf.killed() || !rf.isLeader() || args.Term != rf.Term || !flag {
				return
			}

			DPrintf("[raft-%v %v %v] 收到%v的心跳回复: %v \n", rf.me, rf.getRole(), rf.Term, to, reply.Result)

			if reply.Term > rf.Term {
				rf.changeToFollower(reply.Term)
			} else if reply.Result {
				rf.NextIndex[to] = currMaxIdx + 1
				rf.MatchIndex[to] = currMaxIdx
				if args.Entries == nil {
					return
				}
				// 找到过半的最大日志idx
				var arr []int
				for idx, n := range rf.MatchIndex {
					if idx != rf.me {
						arr = append(arr, n)
					}
				}
				sort.Ints(arr)
				if idx := arr[len(arr)/2]; idx >= 0 && rf.Log[idx].Term == rf.Term && idx > rf.CommittedIndex {
					DPrintf("[raft-%v %v %v] 更新 commitIdx, from %v to %v \n", rf.me, rf.getRole(), rf.Term, rf.CommittedIndex, idx)
					rf.CommittedIndex = idx
				}
				rf.applyMsg()
			} else if len(rf.Log) > 0 {
				// 找到前一个term的日志
				i := args.PrevLogIndex
				min := rf.MatchIndex[to] + 1
				for i > min && rf.Log[i].Term == args.PrevLogTerm {
					i--
				}
				rf.NextIndex[to] = i
			}
			rf.persist()
		}(rf.me, idx, rf.Term)
	}
	rf.mu.Unlock()
	time.Sleep(50 * time.Millisecond)
}

func (rf *Raft) maintainsFollower() {
	defer rf.mu.Unlock()
	if rf.checkTimeout() {
		DPrintf("[raft-%v %v %v] 心跳超时. now = %v", rf.me, rf.getRole(), rf.Term, time.Now().Local())
		rf.changeToCandidate()
	} else {
		time.Sleep(10 * time.Millisecond)
	}
}

func (rf *Raft) maintainsCandidate() {
	// 检查超时
	if !rf.checkTimeout() {
		rf.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
		return
	}

	// 发送投票RPC
	rf.VotedFor = rf.me
	rf.Term += 1
	DPrintf("[raft-%v %v %v] == 发起投票RPC == \n", rf.me, rf.getRole(), rf.Term)
	maxTerm, sumVotes := rf.Term, len(rf.peers)
	validVoteReply := make(chan *RequestVoteReply)
	args := RequestVoteArgs{Peer: rf.me, Term: rf.Term, LastLogTerm: rf.maxLogTerm(), LastLogIndex: rf.maxLogIdx()}
	for idx := range rf.peers {
		if rf.me == idx {
			continue
		}
		go func(from int, to int, term int32) {
			reply := RequestVoteReply{Peer: to, Term: term, VoteGranted: -1}
			_ = rf.sendRequestVote(to, &args, &reply)
			DPrintf("[raft-%v-%v-%v] 收到%v投票回复 = %v .\n", rf.me, rf.getRole(), rf.Term, to, reply)
			validVoteReply <- &reply
		}(rf.me, idx, rf.Term)
	}
	rf.mu.Unlock()

	// 处理投票回复
	voteReplyCount, acceptVotes, rejectVotes, halfNumber := 1, 1, 0, sumVotes/2
	for {
		select {
		case reply := <-validVoteReply:
			voteReplyCount++

			// 根据reply进行处理
			if reply.VoteGranted == Success {
				acceptVotes++
			} else if reply.VoteGranted == TermTooSmall || reply.VoteGranted == LogTooSmall || reply.VoteGranted == HaveVoted {
				rejectVotes++
				maxTerm = maxInt32(maxTerm, reply.Term)
			}

			// 判断是否进入退出条件
			if voteReplyCount == sumVotes || acceptVotes > halfNumber || rejectVotes > halfNumber {
				goto VotedDone
			}
		}
	}

	//汇总投票结果
VotedDone:
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if rf.killed() || !rf.isCandidate() {
		DPrintf("[raft-%v-%v-%v] 竞选请求过期.\n", rf.me, rf.getRole(), rf.Term)
	} else if maxTerm > rf.Term {
		DPrintf("[raft-%v-%v-%v] 放弃竞选.\n", rf.me, rf.getRole(), rf.Term)
		rf.changeToFollower(maxTerm)
	} else if rf.isQuorum(acceptVotes) {
		DPrintf("[raft-%v-%v-%v] == 投票通过: 总票数 = %v. 赞同票数 = %v. 有效票数 = %v. == \n", rf.me, rf.getRole(), rf.Term, sumVotes, acceptVotes, voteReplyCount)
		rf.changeToLeader()
	} else {
		DPrintf("[raft-%v-%v-%v] == 投票未通过: 总票数 = %v. 赞同票数 = %v. 有效票数 = %v. == \n", rf.me, rf.getRole(), rf.Term, sumVotes, acceptVotes, voteReplyCount)
		rf.updateVoteTime()
	}

}

func (rf *Raft) isQuorum(accept int) bool {
	return accept > len(rf.peers)/2
}

func (rf *Raft) applyMsg() {
	for idx := rf.LastAppliedIndex + 1; idx <= rf.CommittedIndex; idx++ {
		msg := ApplyMsg{Command: rf.Log[idx].Command, CommandValid: true, CommandIndex: idx + 1}
		DPrintf("[raft-%v %v %v] 应用log到状态机, msg = {idx : %v, command : %v, term : %v} \n", rf.me, rf.getRole(), rf.Term, msg.CommandIndex-1, msg.Command, rf.Log[idx].Term)
		rf.Apply <- msg
	}
	rf.LastAppliedIndex = rf.CommittedIndex
}

func (rf *Raft) lastLog() *LogEntry {
	if len(rf.Log) > 0 {
		return &rf.Log[len(rf.Log)-1]
	}
	return nil
}

func (rf *Raft) maxLogTerm() int32 {
	lastLog := rf.lastLog()
	if lastLog == nil {
		return -1
	}
	return lastLog.Term
}

func (rf *Raft) maxLogIdx() int {
	lastLog := rf.lastLog()
	if lastLog == nil {
		return -1
	}
	return lastLog.Index - 1
}
