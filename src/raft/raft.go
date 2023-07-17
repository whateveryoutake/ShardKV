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
import "sync"
import "sync/atomic"
import "../labrpc"
import "log"
import "time"
import "math/rand"

import "bytes"
import "../labgob"

const electionTimeout int = 200

//
// as each Raft peer becomes aware that successive log entries are committed,
// the peer should send an ApplyMsg to the service (or tester) on the same server,
// via the applyCh passed to Make().
// set CommandValid to true to indicate that the ApplyMsg contains a newly committed log entry.
//
// in Lab 3 you'll want to send other kinds of messages (e.g.,
// snapshots) on the applyCh; at that point you can add fields to
// ApplyMsg, but set CommandValid to false for these other uses.
//
// 将连续的日志生成快照，发送到applyCh
type ApplyMsg struct {
	CommandValid      bool
	Command           interface{}
	CommandIndex      int
	LastIncludedIndex int
}

type Role int32

const (
	Role_Leader    Role = 0
	Role_Candidate Role = 1
	Role_Follower  Role = 2
)

type Log struct {
	Term    int
	Index   int
	Command interface{}
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

	applyChan chan ApplyMsg

	role              Role
	lastHeartBeatTime time.Time
	lastElectionTime  time.Time
	timeout           int
	// Your data here (2A, 2B, 2C).
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.

	// Persistent state
	currentTerm       int
	votedFor          int
	log               []Log
	lastIncludedIndex int
	lastIncludedTerm  int
	// Volatile state on all servers
	commitIndex int //已知被提交的最高日志条目的索引(单调增加)
	lastApplied int //已应用于状态机的最高日志条目的索引(单调增加)

	// Volatile state on leader
	nextIndex  []int //对于每个服务器，要发送给该服务器的下一个日志条目的索引（初始化为领导者的最后一个日志索引+1）
	matchIndex []int //对于每个服务器，已知在服务器上复制的最高日志条目的索引（初始化为0，单调增加）
}

// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {

	var term int
	var isleader bool
	rf.mu.Lock()
	defer rf.mu.Unlock()
	term = rf.currentTerm
	isleader = rf.role == Role_Leader

	// Your code here (2A).
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
	e.Encode(rf.log)
	e.Encode(rf.lastIncludedIndex)
	e.Encode(rf.lastIncludedTerm)
	data := w.Bytes()
	rf.persister.SaveRaftState(data)
	// w := new(bytes.Buffer)
	// e := labgob.NewEncoder(w)
	// e.Encode(rf.xxx)
	// e.Encode(rf.yyy)
	// data := w.Bytes()
	// rf.persister.SaveRaftState(data)
}

//
// restore previously persisted state.
//
func (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
	r := bytes.NewBuffer(data)
	d := labgob.NewDecoder(r)
	var logItems []Log
	var currentTerm int
	var votedFor int
	var lastIncludedIndex int
	var lastIncludedTerm int

	if d.Decode(&currentTerm) != nil ||
		d.Decode(&votedFor) != nil ||
		d.Decode(&logItems) != nil ||
		d.Decode(&lastIncludedIndex) != nil ||
		d.Decode(&lastIncludedTerm) != nil {
		log.Fatalf("Unable to read persisted state")
	} else {
		rf.currentTerm = currentTerm
		rf.votedFor = votedFor
		rf.log = logItems
		rf.lastIncludedIndex = lastIncludedIndex
		rf.lastIncludedTerm = lastIncludedTerm
		// do not apply the log in the snapshot
		rf.lastApplied = rf.lastIncludedIndex
	}
	// Your code here (2C).
	// Example:
	// r := bytes.NewBuffer(data)
	// d := labgob.NewDecoder(r)
	// var xxx
	// var yyy
	// if d.Decode(&xxx) != nil ||
	//    d.Decode(&yyy) != nil {
	//   error...
	// } else {
	//   rf.xxx = xxx
	//   rf.yyy = yyy
	// }
}

//将快照信息放入消息队列
func (rf *Raft) readSnapshot() {

	msg := ApplyMsg{
		CommandValid:      false,
		Command:           rf.persister.ReadSnapshot(),
		LastIncludedIndex: rf.lastIncludedIndex,
	}
	rf.applyChan <- msg

}

// 返回快照和最后索引
func (rf *Raft) GetSnapshot() ([]byte, int) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	return rf.persister.ReadSnapshot(), rf.lastIncludedIndex
}

//
// example RequestVote RPC arguments structure.
// field names must start with capital letters!
// 候选人为收集选票而调用
//
type RequestVoteArgs struct {
	Term         int //候选人的任期号
	CandidateId  int //请求选票的候选人的 Id
	LastLogIndex int //候选人的最后日志条目的索引值
	LastLogTerm  int //候选人最后日志条目的任期号
	// Your data here (2A, 2B).
}

//
// example RequestVote RPC reply structure.
// field names must start with capital letters!
//
type RequestVoteReply struct {
	// Your data here (2A).
	Term        int  //当前任期号，候选人会更新自己的任期号
	VoteGranted bool //true 表示候选人获得了选票
}

type InstallSnapshotArgs struct {
	Term              int    //领导人的任期号
	LeaderId          int    //领导人的 Id,以便于跟随者重定向请求
	LastIncludedIndex int    //快照会替换所有的条目，直到并包括这个索引
	LastIncludedTerm  int    //快照中包含的最后日志条目的任期号
	Data              []byte //快照分块的原始字节，从偏移量开始
}

type InstallSnapshotReply struct {
	Term int
}

// 由领导者调用，向跟随者发送快照的分块。领导者总是按顺序发送分块。
// args包含leader的相关信息
func (rf *Raft) InstallSnapshot(args *InstallSnapshotArgs, reply *InstallSnapshotReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	// 更新term
	if args.Term > rf.currentTerm {
		rf.BecomeFollower(args.Term)
	}
	reply.Term = rf.currentTerm

	if args.Term == rf.currentTerm {
		rf.lastHeartBeatTime = time.Now()
		// Candidate收到leader发送的快照，变成Follower
		if rf.role == Role_Candidate {
			rf.BecomeFollower(args.Term)
		}
		// 如果leader的快照更新
		if args.LastIncludedIndex > rf.lastIncludedIndex {
			DPrintf("Follower %v get installSnapshot RPC at lastIncludedIndex %v lastApplied %v", rf.me, args.LastIncludedIndex, rf.lastApplied)
			rf.lastIncludedIndex = args.LastIncludedIndex
			rf.lastIncludedTerm = args.LastIncludedTerm
			// important
			// 应用于状态机的lastApplied更新为lastIncludedIndex
			rf.lastApplied = rf.lastIncludedIndex
			// lastIncludedIndex及之前的日志删除
			rf.logTruncate(args.LastIncludedIndex)
			w := new(bytes.Buffer)
			e := labgob.NewEncoder(w)
			e.Encode(rf.currentTerm)
			e.Encode(rf.votedFor)
			e.Encode(rf.log)
			e.Encode(rf.lastIncludedIndex)
			e.Encode(rf.lastIncludedTerm)
			state := w.Bytes()
			// 存储当前状态和日志
			rf.persister.SaveStateAndSnapshot(state, args.Data)
			// 将快照信息放入消息队列
			rf.readSnapshot()
		}

	}

}

func (rf *Raft) setNewTerm(term int) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	rf.currentTerm = term
	return
}

func (rf *Raft) getHeartBeatTime() time.Time {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.lastHeartBeatTime
}

func (rf *Raft) getElectionTime() time.Time {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.lastElectionTime
}

//获取i在log中离起始点index的距离
func (rf *Raft) getRealLogIndex(i int) int {
	if len(rf.log) > 0 {
		startIndex := rf.log[0].Index
		lastIndex, _ := rf.getLastLogInfo() // ok
		//	fmt.Printf("Start index: %v\n", startIndex)
		if startIndex <= i && i <= lastIndex {
			return i - startIndex
		}
	}
	return -1
}

// discard log before index (including index)
func (rf *Raft) logTruncate(index int) {
	if len(rf.log) == 0 {
		return
	} else if len(rf.log) > 0 {
		lastLogIndex, _ := rf.getLastLogInfo()
		if index > lastLogIndex {
			rf.log = []Log{}
		} else {
			realIndex := rf.getRealLogIndex(index)
			rf.log = rf.log[realIndex+1:]
			DPrintf("Server %v truncate the log at index %v, realIndex: %v lastIndex: %v log len: %v", rf.me, index, realIndex, lastLogIndex, len(rf.log))

		}
	}
}

// 根据传入的lastApplied，保存状态和快照
// 将最新快照更新为lastApplied位置
func (rf *Raft) GenerateSnapshot(snapshot []byte, lastApplied int) {
	rf.mu.Lock()
	// 如果lastApplied > lastIncludedIndex
	// 更新lastIncludedIndex和lastIncludedTerm为lastApplied对应项
	if lastApplied > rf.lastIncludedIndex {
		DPrintf("Server %v generate snapshot at index lastApplied %v", rf.me, lastApplied)
		rf.lastIncludedIndex = lastApplied
		rf.lastIncludedTerm = rf.log[rf.getRealLogIndex(lastApplied)].Term
		//删除lastApplied及之前的日志
		rf.logTruncate(lastApplied)
		w := new(bytes.Buffer)
		e := labgob.NewEncoder(w)
		e.Encode(rf.currentTerm)
		e.Encode(rf.votedFor)
		e.Encode(rf.log)
		e.Encode(rf.lastIncludedIndex)
		e.Encode(rf.lastIncludedTerm)
		state := w.Bytes()
		rf.mu.Unlock()
		// 保存状态和快照
		rf.persister.SaveStateAndSnapshot(state, snapshot)
		return
	}
	rf.mu.Unlock()
}

// 返回state的长度
func (rf *Raft) GetRaftStateSize() int {
	return rf.persister.RaftStateSize()
}

//将已提交的日志应用于状态机
func (rf *Raft) applyLog() {
	for !rf.killed() {
		rf.mu.Lock()
		//有些日志已经提交但是还未应用于状态机
		//增大lastApplied,把lastApplied位置的log应用于状态机
		if rf.commitIndex > rf.lastApplied {
			DPrintf("Applying log Instance %v: len %v commit index %v lastApplied %v", rf.me, rf.lastIncludedIndex+len(rf.log), rf.commitIndex, rf.lastApplied)
			rf.lastApplied += 1
			realIndex := rf.getRealLogIndex(rf.lastApplied)
			msg := ApplyMsg{
				CommandValid: true,
				CommandIndex: rf.log[realIndex].Index,
				Command:      rf.log[realIndex].Command,
			}
			rf.mu.Unlock()
			rf.applyChan <- msg
		} else {
			rf.mu.Unlock()
		}
	}
}

func (rf *Raft) mainLoop() {
	for !rf.killed() {

		timeout := getRandTimeout()
		time.Sleep(time.Millisecond * time.Duration(rf.timeout))

		rf.mu.Lock()
		role := rf.role
		rf.mu.Unlock()

		switch role {
		// leader does not have something needed to be check routinely
		// when the instance wins the election, the StartElection will open the heatbeat thread
		// using for loop to create many heatbeat threads is strange
		case Role_Candidate:
			{
				//DPrintf("Instance %v is the candidate now", rf.me)
				sub := time.Now().Sub(rf.getElectionTime())
				//选举超时，成为候选人开始选举
				if sub > time.Duration(timeout)*time.Millisecond {
					rf.mu.Lock()
					rf.BecomeCandidate()
					rf.mu.Unlock()
					go rf.StartElection()
					DPrintf("Instance %v starts new election (candidate->candidate)", rf.me)
				}
			}
		case Role_Follower:
			{
				//	DPrintf("Instance %v is the follower now", rf.me)
				sub := time.Now().Sub(rf.getHeartBeatTime())
				//选举超时，成为候选人开始选举
				if sub > time.Duration(timeout)*time.Millisecond {
					DPrintf("Instance %v starts election (follower->candidate)", rf.me)
					rf.mu.Lock()
					rf.BecomeCandidate()
					rf.mu.Unlock()
					go rf.StartElection()
				}
			}
		}
	}
}

type AppendEntriesArgs struct {
	Term         int
	LeaderId     int
	PrevLogIndex int
	PrevLogTerm  int
	Entries      []Log
	LeaderCommit int
	IsHeartBeat  bool
}

type AppendEntriesReply struct {
	Term      int
	Success   bool
	XTerm     int
	XIndex    int
	XLen      int
	NextIndex int
}

//leader发送心跳给各个peer
func (rf *Raft) StartAppendEntries(is bool) {
	for !rf.killed() {
		rf.mu.Lock()
		// nextIndex >= 1
		// 如果不是leader直接返回
		if rf.role != Role_Leader {
			rf.mu.Unlock()
			return
		}
		rf.mu.Unlock()
		//开始发送心跳
		DPrintf("Instance %v is the leader and sending entries\n", rf.me)
		for i := 0; i < len(rf.peers); i++ {
			if i != rf.me {
				go func(id int) {
					for !rf.killed() {
						rf.mu.Lock()
						// nextIndex >= 1
						if rf.role != Role_Leader {
							rf.mu.Unlock()
							return
						}
						//获取最后一个日志的索引
						lastLogIndex, _ := rf.getLastLogInfo()
						if rf.nextIndex[id] >= rf.lastIncludedIndex+1 {
							DPrintf("Server %v send log interval [%v, %v]", rf.me, rf.nextIndex[id], lastLogIndex)
							DPrintf("Server %v Last included index: %v ", rf.me, rf.lastIncludedIndex)
							prevLogIndex := rf.nextIndex[id] - 1
							prevLogTerm := 0
							//更新prevLogTerm
							//如果已发送日志的前一条 > leader的lastIncludedIndex
							//更新为前一条日志的Term
							if prevLogIndex > rf.lastIncludedIndex { // && prevLogIndex <= lastLogIndex {
								prevLogTerm = rf.log[rf.getRealLogIndex(prevLogIndex)].Term
								//相等的话,更新为lastIncludedTerm
							} else if prevLogIndex == rf.lastIncludedIndex {
								prevLogTerm = rf.lastIncludedTerm
							}

							entries := []Log{}
							//将未发送的日志加入entries
							//R4: 添加日志中任何尚未出现的新条目
							for j := rf.nextIndex[id]; j <= lastLogIndex; j++ {
								entries = append(entries, rf.log[rf.getRealLogIndex(j)])
							}

							rf.mu.Unlock()
							args := AppendEntriesArgs{
								Term:         rf.currentTerm,
								LeaderId:     rf.me,
								PrevLogTerm:  prevLogTerm,
								PrevLogIndex: prevLogIndex,
								Entries:      entries,
								LeaderCommit: rf.commitIndex,
								IsHeartBeat:  is,
							}

							reply := AppendEntriesReply{}
							//peer调用AppendEntries
							ok := rf.sendAppendEntries(id, &args, &reply)
							//添加日志条目或者心跳发送成功
							if ok {
								rf.mu.Lock()
								//reply的term大于当前term,成为Follower
								if reply.Term > rf.currentTerm {
									rf.BecomeFollower(reply.Term)
									rf.mu.Unlock()
									return
								}
								// only update commitindex when success
								// otherwise the commitIndex may fall back
								// currentTerm没变并且还是Leader的话
								if args.Term == rf.currentTerm && rf.role == Role_Leader {
									//如果成功：为跟随者更新nextIndex和matchIndex
									//以及自身的matchIndex
									if reply.Success {
										rf.matchIndex[id] = prevLogIndex + len(entries)
										rf.nextIndex[id] = rf.matchIndex[id] + 1

										rf.matchIndex[rf.me] = rf.lastIncludedIndex + len(rf.log)
										nextCommitIdx := rf.commitIndex
										for i := 0; i < len(rf.peers); i++ {
											DPrintf("Leader %v Matchindex[%v]=%v", rf.me, i, rf.matchIndex[i])
										}
										//遍历未提交的日志
										for i := rf.commitIndex + 1; i <= rf.matchIndex[rf.me]; i++ {
											vote := 0
											//遍历peer,如果有matchIndex(已提交Index)比i大，计票数+1
											for j := 0; j < len(rf.peers); j++ {
												if rf.matchIndex[j] >= i {
													vote += 1
												}
											}
											//如果存在一个N，使得N>commitIndex，大多数的matchIndex[i]≥N
											//并且log[N].term == currentTerm：设置commitIndex = N
											if vote >= len(rf.peers)/2+1 {
												realIndex := rf.getRealLogIndex(i)
												//realIndex存在并且对应term等于currentTerm
												//或者realIndex和lastIncludedIndex相等并且currentTerm等于lastIncludedTerm
												//更新nextCommitIdx为i
												if realIndex != -1 && rf.log[realIndex].Term == rf.currentTerm || realIndex == rf.lastIncludedIndex && rf.currentTerm == rf.lastIncludedTerm {
													nextCommitIdx = i
												}
											}
										}

										DPrintf("Leader %v commitIndex: %v", rf.me, nextCommitIdx)

										rf.commitIndex = nextCommitIdx
										rf.mu.Unlock()
										break
									} else {
										DPrintf("Before fall back : nextIndex[%v]= %v", id, rf.nextIndex[id])
										//如果reply的nextIndex不为空，更新为nextIndex
										if reply.NextIndex != -1 {
											rf.nextIndex[id] = reply.NextIndex
											//否则若XLen不为空，更新为XLen+1
										} else if reply.XLen != -1 {
											rf.nextIndex[id] = reply.XLen + 1
											//否则更新为XIndex
										} else {
											rf.nextIndex[id] = reply.XIndex
											//从最后开始遍历, 找到第一个Term小于等于reply.Xterm的index
											for i := len(rf.log) - 1; i >= 0; i-- {
												//当XTerm > 当前日志条目的Term时， 退出
												if rf.log[i].Term < reply.XTerm {
													break
												}
												//相等时，更新为对应的index
												if rf.log[i].Term == reply.XTerm {
													rf.nextIndex[id] = rf.log[i].Index
													break
												}
											}
										}
										DPrintf("After fall back : nextIndex[%v]= %v", id, rf.nextIndex[id])
									}
								}
								rf.mu.Unlock()
							}
							//rf.nextIndex[id] < rf.lastIncludedIndex+1
						} else { // send install snapshot RPC
							args := InstallSnapshotArgs{
								Term:              rf.currentTerm,
								LeaderId:          rf.me,
								Data:              rf.persister.ReadSnapshot(),
								LastIncludedIndex: rf.lastIncludedIndex,
								LastIncludedTerm:  rf.lastIncludedTerm}
							reply := InstallSnapshotReply{}
							rf.mu.Unlock()
							ok := rf.sendInstallSnapshot(id, &args, &reply)
							rf.mu.Lock()
							if ok {
								if reply.Term > rf.currentTerm {
									rf.BecomeFollower(reply.Term)
									rf.mu.Unlock()
									return
								}
								if args.Term == rf.currentTerm && rf.role == Role_Leader {
									rf.nextIndex[id] = args.LastIncludedIndex + 1
									rf.matchIndex[id] = args.LastIncludedIndex
								}
							}
							rf.mu.Unlock()
						}
					}
				}(i)
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

}

//peer处理leader的日志条目或心跳信息,args中是leader的相关信息
func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.mu.Lock()

	defer rf.mu.Unlock()
	// WRONG IMPLEMENTATION
	// if rf.role == rf.currentTerm then covert to follower
	// 如果args的term比当前term大，更新term
	if args.Term > rf.currentTerm {
		rf.BecomeFollower(args.Term)
	}
	reply.Term = rf.currentTerm
	reply.Success = false
	reply.XLen = -1
	reply.XTerm = -1
	reply.XIndex = -1
	reply.NextIndex = -1
	if args.Term == rf.currentTerm {
		// only heartbeat with latest term is valid
		// 收到心跳，重置时间
		rf.lastHeartBeatTime = time.Now()
		// CORRECT IMPLEMENTATION (found by Test)
		// When the leader's term is greater or equals to candidate's term
		// candidate can go to follower
		// do not covert to follower when receive the heartbeat
		//收到心跳的candidate立马转变为Follower
		if rf.role == Role_Candidate {
			rf.BecomeFollower(args.Term)
		}
		//获取最后一个日志条目的index
		lastLogIndex, _ := rf.getLastLogInfo()
		// leader日志最后的Index更小
		// 如果一个跟随者的最后一个日志索引 ≥ nextIndex：发送AppendEntries RPC包含从nextIndex开始的日志条目???
		if args.PrevLogIndex <= lastLogIndex {
			//获取leader最后的logIndex在该server对应的index
			realPrevLogIndex := rf.getRealLogIndex(args.PrevLogIndex)

			logTerm := 0
			//给logTerm赋值
			//realPrevLogIndex不为空
			//直接赋值为realPrevLogIndex对应的term
			if realPrevLogIndex != -1 {
				logTerm = rf.log[realPrevLogIndex].Term
				//如果和lastIncludedIndex相等，赋值为lastIncludedTerm
			} else if args.PrevLogIndex == rf.lastIncludedIndex {
				logTerm = rf.lastIncludedTerm
				//如果比lastIncludedIndex小
			} else if args.PrevLogIndex < rf.lastIncludedIndex {
				reply.Success = false
				reply.NextIndex = rf.lastIncludedIndex + 1
				return
			}
			//如果logTerm和leader的最后日志term相等，回复成功
			if logTerm == args.PrevLogTerm {
				reply.Success = true
				//从头遍历日志，对leader每条待添加日志的index求出realIndex
				for idx := 0; idx < len(args.Entries); idx++ {
					realIndex := rf.getRealLogIndex(args.Entries[idx].Index)
					// has conflict
					if realIndex != -1 {
						// 要是term不相等，发生冲突
						// R3: 如果一个现有的条目与一个新的条目相冲突（相同的索引但不同的任期），删除现有的条目和后面所有的条目
						if args.Entries[idx].Term != rf.log[realIndex].Term {
							rf.log = append(rf.log[:realIndex], args.Entries[idx:]...)
							break
						}
					} else { // no conflict or log does not exists
						rf.log = append(rf.log, args.Entries[idx])
					}
				}

				rf.persist()
				//R5: 如果leaderCommit > commitIndex
				//设置commitIndex = min(leaderCommit, 最后一个新条目的索引)
				if args.LeaderCommit > rf.commitIndex {
					lastEntryIndex, _ := rf.getLastLogInfo() // ok
					if args.LeaderCommit < lastEntryIndex {
						rf.commitIndex = args.LeaderCommit
					} else {
						rf.commitIndex = lastEntryIndex
					}
				}
				//R2: 如果日志在prevLogIndex处不包含与prevLogTerm匹配的条目，则返回false
			} else {
				//logTerm赋值给reply的XTerm
				reply.XTerm = logTerm
				index := -1
				//如果有跟logTerm相等的log，找到index
				for i := 0; i < len(rf.log); i++ {
					if rf.log[i].Term == reply.XTerm {
						index = rf.log[i].Index
						break
					}
				}
				//设为XIndex
				reply.XIndex = index
			}
			// args.PrevLogIndex > lastLogIndex
		} else {
			reply.XLen = len(rf.log) + rf.lastIncludedIndex
		}
	}

	DPrintf("HeartBeat: %v Instance %v receive rpc from %v. HeartBeat Term:%v My Term: %v Result: %v Entries size: %v Commit Index: %v Log Length: %v PreLogIndex: %v",
		args.IsHeartBeat, rf.me, args.LeaderId, args.Term, rf.currentTerm, reply.Success, len(args.Entries), rf.commitIndex, rf.lastIncludedIndex+len(rf.log), args.PrevLogIndex)

}

//
// example RequestVote RPC handler.
// 返回lastLogIndex和lastLogTerm
// 如果log为空，返回lastIncludedIndex和lastIncludedTerm
func (rf *Raft) getLastLogInfo() (int, int) {
	// use it with lock
	lastLogIndex := 0
	lastLogTerm := 0
	if len(rf.log) > 0 {
		lastLogIndex = rf.log[len(rf.log)-1].Index
		lastLogTerm = rf.log[len(rf.log)-1].Term
	} else if len(rf.log) == 0 {
		lastLogIndex = rf.lastIncludedIndex
		lastLogTerm = rf.lastIncludedTerm
	}
	return lastLogIndex, lastLogTerm
}

func (rf *Raft) StartElection() {
	rf.mu.Lock()
	//不是候选人直接返回
	if rf.role != Role_Candidate {
		rf.mu.Unlock()
		return
	}
	// vote to itself
	voteCount := 1
	lastLogIndex, lastLogTerm := rf.getLastLogInfo() // ok
	args := RequestVoteArgs{
		Term:         rf.currentTerm,
		CandidateId:  rf.me,
		LastLogIndex: lastLogIndex,
		LastLogTerm:  lastLogTerm,
	}
	rf.mu.Unlock()

	for i := 0; i < len(rf.peers); i++ {
		if i != rf.me {
			go func(id int) {
				//给每个peer发送请求投票信息
				reply := RequestVoteReply{}
				ok := rf.sendRequestVote(id, &args, &reply)
				if ok {
					DPrintf("Instance %v  gets vote reply from %v, result %v", rf.me, id, reply.VoteGranted)
					//有回复的term比自己的大，成为Follower
					if reply.Term > args.Term {
						rf.mu.Lock()
						rf.BecomeFollower(reply.Term)
						rf.mu.Unlock()
						return
					}
					rf.mu.Lock()
					//如果term没有更新并且角色为Candidate,计票数+1
					if rf.currentTerm == args.Term && rf.role == Role_Candidate {
						if reply.VoteGranted {
							voteCount += 1
						}
						DPrintf("Instance %v  gets vote count: %v", rf.me, voteCount)
						//计票数大于一半，成为leader
						//发送心跳
						if voteCount >= len(rf.peers)/2+1 {
							DPrintf("Instance %d wins the election (candidate -> leader)", rf.me)
							rf.BecomeLeader()
							rf.mu.Unlock()
							go rf.StartAppendEntries(true)
							return
						}

					}
					rf.mu.Unlock()

				}
			}(i)
		}
	}

}

func (rf *Raft) BecomeLeader() {
	rf.role = Role_Leader
	lastIndex := 0
	if len(rf.log) > 0 {
		lastIndex = rf.log[len(rf.log)-1].Index
	} else if len(rf.log) == 0 {
		lastIndex = rf.lastIncludedIndex
	}
	//所有nextIndex更新为lastIndex+1
	//所有matchIndex更新为0
	for i := 0; i < len(rf.peers); i++ {
		rf.nextIndex[i] = lastIndex + 1
		rf.matchIndex[i] = 0
	}
}

//成为候选人，给自己投一票，计数+1，选举时间重置，随机取过期时间，保存到磁盘上
func (rf *Raft) BecomeCandidate() {
	rf.timeout = getRandTimeout()
	rf.role = Role_Candidate
	rf.currentTerm += 1
	rf.lastElectionTime = time.Now()
	rf.votedFor = rf.me
	rf.persist()
}

//成为候选人，投票取消，心跳时间重置，随机取过期时间，保存到磁盘上
func (rf *Raft) BecomeFollower(term int) {
	rf.timeout = getRandTimeout()
	rf.lastHeartBeatTime = time.Now() // reset timeout!
	rf.role = Role_Follower
	rf.votedFor = -1
	rf.currentTerm = term
	rf.persist()
}

// Follower收到候选人发送的投票请求
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here (2A, 2B).
	rf.mu.Lock()
	defer rf.mu.Unlock()
	DPrintf("Instance %v receive vote request from %v. Request Term:%v My Term: %v",
		rf.me, args.CandidateId, args.Term, rf.currentTerm)
	//candidate的term比自己的大，成为Follower
	if args.Term > rf.currentTerm {
		rf.BecomeFollower(args.Term)
	}
	reply.Term = rf.currentTerm
	reply.VoteGranted = false

	//R1:candidate的term比自己的小，不给他投票
	//R2: 否则若还没投票，或者已经给candidate投票，获取最后一个日志的信息
	if args.Term < rf.currentTerm {
		reply.VoteGranted = false
		//如果 votedFor 是 null 或 candidateId，
	} else if rf.votedFor == -1 || rf.votedFor == args.CandidateId {
		lastLogIndex, lastLogTerm := rf.getLastLogInfo() // ok
		// at least as up-to-date as receiver's log
		DPrintf("LastLogIndex %v LastLogTerm %v, my Index %v, my Term %v",
			args.LastLogIndex, args.LastLogTerm, lastLogIndex, lastLogTerm)
		//R2: 并且候选人的日志至少与接收人的日志一样新，则投票
		//如果候选人的term与其相同，index更大，或者term更大
		//同意给candidate投票，重置心跳时间，保存到磁盘
		if args.LastLogIndex >= lastLogIndex && args.LastLogTerm == lastLogTerm || args.LastLogTerm > lastLogTerm {
			rf.lastHeartBeatTime = time.Now()
			rf.votedFor = args.CandidateId
			reply.VoteGranted = true
			rf.persist()
			DPrintf("Instance %v grants vote to %v", rf.me, rf.votedFor)
		}

	}

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

//leader调用以复制日志条目或发送心跳
func (rf *Raft) sendAppendEntries(server int, args *AppendEntriesArgs, reply *AppendEntriesReply) bool {
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
	return ok
}

func (rf *Raft) sendInstallSnapshot(server int, args *InstallSnapshotArgs, reply *InstallSnapshotReply) bool {
	ok := rf.peers[server].Call("Raft.InstallSnapshot", args, reply)
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
	//command(op)会放入log中
	index := -1
	term := -1
	isLeader := true

	// Your code here (2B).
	rf.mu.Lock()

	term = rf.currentTerm
	isLeader = rf.role == Role_Leader

	rf.mu.Unlock()

	if isLeader {
		rf.mu.Lock()
		lastLogIndex, _ := rf.getLastLogInfo() //ok
		index = lastLogIndex + 1
		entry := Log{
			Term:    rf.currentTerm,
			Index:   index,
			Command: command,
		}
		rf.log = append(rf.log, entry)
		rf.persist()
		DPrintf("Instance %v add new log %v %v ", rf.me, index, rf.currentTerm)
		rf.mu.Unlock()
	}

	return index, term, isLeader
}

// Start(command) asks Raft to start the processing to append the command to the replicated log.
// Start() should return immediately, without waiting for the log appends to complete.
// The service expects your implementation to send an ApplyMsg for each newly committed log entry to the applyCh channel argument to Make().

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
	rf.applyChan = applyCh

	// Your initialization code here (2A, 2B, 2C).
	rf.role = Role_Follower
	rf.timeout = getRandTimeout()
	rf.currentTerm = 0
	rf.votedFor = -1
	rf.lastHeartBeatTime = time.Now()
	rf.lastElectionTime = time.Now()
	rf.log = []Log{}
	rf.lastIncludedIndex = 0
	rf.lastIncludedTerm = 0

	rf.commitIndex = 0
	rf.lastApplied = 0

	rf.nextIndex = []int{}
	rf.matchIndex = []int{}
	for i := 0; i < len(rf.peers); i++ {
		rf.nextIndex = append(rf.nextIndex, 1)
		rf.matchIndex = append(rf.matchIndex, 0)
	}
	/*
		f, err := os.Create(strconv.Itoa(rf.me))
		if err != nil {
			log.Fatalf("error opening file: %v", err)
		}
		log.SetOutput(f)*/
	// initialize from state persisted before a crash*/
	DPrintf("Instance %v starts the main loop, timeout limit: %v", rf.me, rf.timeout)
	rf.readPersist(persister.ReadRaftState())
	//	rf.readSnapshot()
	go rf.mainLoop()
	go rf.applyLog()

	return rf
}

// create a Raft peer
// The me argument is the index of this peer in the peers array.
func getRandTimeout() int {
	rand.Seed(time.Now().UnixNano())
	return electionTimeout + rand.Intn(electionTimeout)
}
