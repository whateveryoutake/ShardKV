## Preview
该项目实现了MIT 6.824(2020)中的四个lab，通过了所有测试用例，包括一些Chanllenge的部分，其中服务器之间通过RPC进行通信，lab2-lab4是一个层层递进的关系
## lab1 MapReduce
有多台服务器，其中一台作为server负责任务的分发以及失效worker的处理，其余都作为worker来调用Map和Reduce函数处理读写文件。  
注意：  
如果worker没有在合理的时间内完成任务(10s)，master应该将任务交给另一个worker来处理；  
worker应该将Map输出的intermediate放在当前目录下的文件中，稍后worker可以将其作为Reduce任务的输入读取  
实现退出的方法，当reduce任务都结束时，master进入Exit状态，创建一个exit状态的任务加入任务队列，worker取出任务，当任务状态为exit直接退出。

## lab2 Raft
raft是一个分布式系统的一致性协议，同时还有一定程度的容错  
复制服务通过在多个服务器上存储状态(即数据)的完整副本来实现容错。复制允许服务继续运行，即使一些服务器遇到故障(崩溃、中断或不稳定的网络)。  
挑战在于，失败可能导致副本保存数据的不同副本。
Raft将客户端请求组织成一个序列，称为日志，并确保所有副本服务器看到相同的日志。每个副本按日志顺序执行客户端请求，并将其应用于服务状态的本地副本。由于所有活动副本都看到相同的日志内容，因此它们都以相同的顺序执行相同的请求，从而继续具有相同的服务状态。如果服务器出现故障，但后来恢复了，raft会负责更新日志。只要至少大多数服务器还活着并且可以相互通信，Raft就会继续运行；否则 Raft将不会取得任何进展，但一旦大多数人能够再次进行通信，它就会从中断的地方重新开始。
主要RPC：  
### StartAppendEntries(is bool) 
由leader调用，参数用于区分初始心跳和后续的AppendEntries操作；  
对每个peer(除自己外)，调用go routine并行发送请求，  
如果nextIndex[id] >= rf.LastIncludedIndex + 1，则发送AppendEntries RPC（需要添加日志条目）
否则发送InstallSnapshot RPC安装快照（更新nextIndex数组）
### AppendEntries(args, reply) 
follower收到的关于leader调用的RPC。  
对于收到的每条Entry，如果发生冲突（index相同而term不同），则删除peer中该index及之后的日志，然后替换成Entries，否则如不在日志中则直接添加
### RequestVote(args)
peer收到candidate的投票请求。  
如果peer也是candidate，则会比较term，若对方的term比自己大，自己转变为follower状态;  
如果对方term比自己大并且自己还没投票（或者恰好投给了对方），同时对方最后日志的信息至少跟自己一样新（term相同，index相等或更大，或term更大）;  
回复的term是两者term的较大值。
### InstallSnapshot(args, reply)
由leader调用，如果nextIndex[id] < rf.LastIncludedIndex + 1,说明nextIndex数组没有及时更新，该方法会更新peer对应的lastIncludedIndex和lastIncludedTerm、lastApplied  
删除lastIncluedIndex之前的日志
## lab3 KVRaft
构建了一个基于Raft的kv存储系统，客户会对服务器发出RPC请求，通过raft达成共识后会返回给用户对应的结果，主要支持的操作有Get(获取键值)、Put(替换键值)以及Append操作(追加键值) ，为了防止日志无限制的增长，还实现了快照功能，使得服务器可以丢弃之前的日志，当服务器重新启动后可以通过安装快照来迅速恢复之前的状态 
Client的结构定义
```
type Clerk struct {
	servers []*labrpc.ClientEnd
	// You will have to modify this struct.  
	lastLeader int        //上一个联络的leader  
	seqNumber  int64      //执行命令的编号  
	id         int64      //客户的编号  
	mu         sync.Mutex //互斥锁  
}
```
### server中重要方法
### Get()及PutAppend()
Client发出请求后调用RPC，服务器会调用某个服务器中对应的方法  
判断服务器是否为leader同时取出对应客户Id的序列号判断是否有效(>=args中序列号，说明该操作已被处理)  
封装op,调用kv.rf.start(op)加入日志，在对应index(lastLogIndex+1)处创建队列并监听（监听成功更新reply），超时返回ErrWrongLeader，更新reply的值
### processLog()
服务器启动后便会启动go routine轮询  
从消息队列取出消息（来源于raft）  
来自applyLog()  
若操作有效(序列号大于客户当前序列号)，根据操作类型对本地db进行操作，更新客户对应的序列号  
取出对应index处的消息队列，将op放入
来自installSnapshot()  
对lastApplied更新
### doSnapshot()
服务器启动后便会启动go routine轮询  
当stateSize超过最大值，调用raft中GenerateSnapshot生成快照
## lab4 ShardKV
在lab2和lab3的基础上继续构建支持分片的KV存储系统，分片是键/值对的子集;例如，所有以“a”开头的键可能是一个分片，所有以“b”开头的键可能是另一个分片。每个shard对应一个group, 每个group又包含多个server。    
分片的优势在于提高性能：不同复制组之间并行操作，组与组之间互不干扰；因此，总系统吞吐量(单位时间内的输入和获取)与组的数量成比例地增加。  
该系统主要由两部分组成： 
* 一系列复制组
每个复制组负责一组分片，由几个服务器组成，这些服务器使用Raft来复制组间的分片。
* shard master
决定哪个复制组应该服务于哪个shard;这些信息称为配置。配置随着时间的推移而变化。客户端通过查询shard master来查找对应key的复制组，而复制组通过查询shard master来查找需要服务的分片。整个系统只有一个shardMaster，使用Raft作为容错服务实现。
### 4A1.Shard Master中client的主要RPC
### Join(servers map[int][] string)
参数是一组映射，从gid到服务器名称列表，功能是创建一个包含新复制组的新配置来做出反应，新的配置应该在组集合中尽可能均匀地划分分片（尽可能少的移动）
### Query(num int)
输入一个configuration num, shard master返回对应的配置config, 如果num为-1或者大于已知最新配置的num，返回最新的配置
### Leave(gid []int)
参数是先前加入的gid列表，shard master会将这些gid从配置组中删除，并将这些组中分片分给其他组（保证尽可能均匀，且移动较少）
### Move(shard int, gid int)
参数是一个分片号和一个gid, shard master将该分片移到该组
### 4A2.Server中主要函数
### ShardKV结构
```
type ShardMaster struct {
	mu      sync.Mutex
	me      int
	rf      *raft.Raft
	applyCh chan raft.ApplyMsg

	// Your data here.
	killed  bool
	configs []Config // indexed by config num
	// record the timestamps
	clients map[int64]int64
	// index in Raft to reply channel
	channels map[int]chan Op
}
```
### Op结构，会放入raft的日志中
```
type Op struct {
	// Your data here.
	Type OpType
	// Join
	JoinServers map[int][]string
	// Leave
	LeaveGIDs []int
	// Move
	MoveShard int
	MoveGID   int
	// Query
	QueryNum    int
	QueryConfig Config

	ClientID int64
	SeqNum   int64
}
```
### Config结构
```
type Config struct {
	Num    int              // config number
	Shards [NShards]int     // shard -> gid
	Groups map[int][]string // gid -> servers[]
}
```
### rebalance函数，将config重新均匀分配
```
func (sm *ShardMaster) rebalance(config *Config) {
	gidArray := make([]int, 0)
	//根据config初始化gidArray
	for k, _ := range config.Groups {
		gidArray = append(gidArray, k)
	}
	sort.Ints(gidArray)
	if len(gidArray) > 0 {
		for i := 0; i < NShards; i++ {
			config.Shards[i] = gidArray[i%len(gidArray)]
		}
	} else {
		config.Shards = [NShards]int{}
	}

}
```
### handleJoinOp 
还原最新config中的group,加入op中JoinServers，rebalance，加入新配置
```
func (sm *ShardMaster) handleJoinOp(op *Op) {

	lastIndex := len(sm.configs) - 1
	config := Config{}

	config.Num = sm.configs[lastIndex].Num + 1
	config.Groups = make(map[int][]string)
	// add new group info
	for k, v := range sm.configs[lastIndex].Groups {
		config.Groups[k] = v
	}

	for k, v := range op.JoinServers {
		config.Groups[k] = v
	}

	sm.rebalance(&config)

	sm.configs = append(sm.configs, config)

	// new shard info
}
```
### handleLeaveOp
还原最新config中的group，删除op中LeaveGids，rebalance，加入新配置
```
func (sm *ShardMaster) handleLeaveOp(op *Op) {
	lastIndex := len(sm.configs) - 1
	config := Config{}

	config.Num = sm.configs[lastIndex].Num + 1
	config.Groups = make(map[int][]string)
	// add new group info
	for k, v := range sm.configs[lastIndex].Groups {
		config.Groups[k] = v
	}

	for _, v := range op.LeaveGIDs {
		delete(config.Groups, v)
	}

	sm.rebalance(&config)
	sm.configs = append(sm.configs, config)
}
```
### handleMoveOp
还原最新config中的groups和shards，把对应gid移到对应shard中，加入新配置
```
func (sm *ShardMaster) handleMoveOp(op *Op) {
	lastIndex := len(sm.configs) - 1
	config := Config{}

	config.Num = sm.configs[lastIndex].Num + 1
	config.Groups = make(map[int][]string)
	// add new group info
	for k, v := range sm.configs[lastIndex].Groups {
		config.Groups[k] = v
	}

	for i, v := range sm.configs[lastIndex].Shards {
		config.Shards[i] = v
	}

	config.Shards[op.MoveShard] = op.MoveGID
	sm.configs = append(sm.configs, config)
}
```
### handleQueryOp
根据传入num返回对应的config
```
func (sm *ShardMaster) handleQueryOp(op *Op) {
	if op.QueryNum == -1 || op.QueryNum > sm.configs[len(sm.configs)-1].Num {
		op.QueryConfig = sm.configs[len(sm.configs)-1]
	} else {
		for i := 0; i < len(sm.configs); i++ {
			if sm.configs[i].Num == op.QueryNum {
				op.QueryConfig = sm.configs[i]
				break
			}
		}
	}

}
```
### server处理调用rpc(Join/Query/Move/Leave等)
大致流程都相似  
上锁，判断当前服务器是否为leader并且操作已经被处理，设置reply的Err和WrongLeader，解锁  
封装op，调用rf.start放入raft的日志中，在对应Index处建立队列，并对其进行监听（超时WrongLeader，监听成功更新reply），上锁删除对应index处的队列
### applyLog
启动服务器后会启动go routine轮询  
取出消息队列中的消息（来自raft,说明已经应用于状态机），上锁，若对应client序列号还未应用，根据op类型进行操作,更新clients的序列号，将op放入index对应的channel，解锁
```
func (sm *ShardMaster) applyLog() {
	for !sm.killed {
		msg := <-sm.applyCh
		op := msg.Command.(Op)
		sm.mu.Lock()
		maxSeq, ok := sm.clients[op.ClientID]
		if !ok || maxSeq < op.SeqNum {
			DPrintf("Server %v applied log at index %v.", sm.me, msg.CommandIndex)
			switch op.Type {
			case OpType_Join:{
					sm.handleJoinOp(&op)
				}
			case OpType_Leave:{
					sm.handleLeaveOp(&op)
				}
			case OpType_Move:{
					sm.handleMoveOp(&op)
				}
			case OpType_Query:{
					sm.handleQueryOp(&op)
				}
			}
			sm.clients[op.ClientID] = op.SeqNum
			ch, ok := sm.channels[msg.CommandIndex]
			delete(sm.channels, msg.CommandIndex)
			sm.mu.Unlock()
			_, isLeader := sm.rf.GetState()
			if ok && isLeader {
				ch <- op
			}
		} else {
			sm.mu.Unlock()
		}
	}
}
```
### 4B1.ShardKV中client调用的RPC
跟lab3中KVRaft基本操作（Get/Put/Append）差不多，根据键值key对应到固定的shard，在每个group中依次访问每个server,更新ck中的config
### 4B2.ShardKV中server主要函数
Op结构
```
type Op struct {
	// Your definitions here.
	// Field names must start with capital letters,
	// otherwise RPC will break.
	OpType         KvOp
	Key            string
	Value          string
	Id             int64
	SeqNum         int64
	Err            Err
	ConfigNumber   int
	MigrationReply GetMigrationReply
	Config         shardmaster.Config
	GCNum          int
	GCShard        int
}
```
ShardKV结构
```
type ShardKV struct {
	mu           sync.Mutex
	me           int
	rf           *raft.Raft
	applyCh      chan raft.ApplyMsg
	make_end     func(string) *labrpc.ClientEnd
	gid          int
	masters      []*labrpc.ClientEnd
	maxraftstate int // snapshot if log grows this big

	// Your definitions here.
	dead        int32
	lastApplied int
	db          [shardmaster.NShards]map[string]string
	// Key: index Value: op
	channels map[int]chan Op
	// clients sequence number
	clients   [shardmaster.NShards]map[int64]int64
	configs   []shardmaster.Config
	oldConfig shardmaster.Config
	pdclient  *shardmaster.Clerk

	availableShards map[int]bool
	oldshards       map[int]map[int]bool

	requiredShards map[int]bool
	// config id -> shard id -> data
	oldshardsData map[int]map[int]map[string]string
	// config id -> shard id -> seq
	oldshardsSeq map[int]map[int]map[int64]int64
	// config id -> shard id
	garbageList map[int]map[int]bool
}
```
### 调用的RPC（Get/Put/Append）
大致与lab3相同，有几点注意  
保证num与最新的config相同（config未发生变化），并且对应的shard是可用的
函数中带有go的表示从服务器开始便启动go routine进行轮询的函数
### pullConfig() go
由leader调用，当出现新的config且requiredShards为空时    
封装op（类型为config），调用rf.start放入raft的日志中
```
func (kv *ShardKV) pullConfig() {
	for !kv.killed() {
		//only leader can take the configuration
		if _, isLeader := kv.rf.GetState(); isLeader {
			nextNum := kv.latestConfig().Num + 1
			cfg := kv.pdclient.Query(nextNum)
			kv.mu.Lock()
			// first condition: add condition here to reduce useless log in Raft
			// second condition: make sure the migration is completed one by one
			if cfg.Num == kv.latestConfig().Num+1 && len(kv.requiredShards) == 0 {
				kv.mu.Unlock()
				op := Op{
					OpType: KvOp_Config,
					Config: cfg}
				kv.rf.Start(op)
			} else {
				kv.mu.Unlock()
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
}
```
### pullShards() go
由leader调用，当requiredShards不为空时调用  
初始化neededShards和oldConfig，设置一组信号量waitGroup  
遍历neededShards go func(shard),取出oldConfig对应shard中的gid和groups  
遍历每个server,封装args,调用ShardKV.GetMigration  
调用且返回成功，封装op，放入raft日志中
```
func (kv *ShardKV) pullShards() {
	for !kv.killed() {
		//only leader can pull shards
		_, isLeader := kv.rf.GetState()
		kv.mu.Lock()
		if isLeader && len(kv.requiredShards) != 0 {
			// make a wait group here
			neededShards := make(map[int]bool)
			for k, v := range kv.requiredShards {
				neededShards[k] = v
			}
			oldConfig := kv.oldConfig.Num

			kv.mu.Unlock()

			wg := sync.WaitGroup{}
			wg.Add(len(neededShards))
			for k, _ := range neededShards {
				// TODO: add pull shards logic
				go func(shard int) {
					gid := kv.configs[oldConfig].Shards[shard]
					group := kv.configs[oldConfig].Groups[gid]
					for i := 0; i < len(group); i++ {
						srv := kv.make_end(group[i])
						args := GetMigrationArgs{
							Num:   kv.configs[oldConfig].Num,
							Shard: shard,
						}
						reply := GetMigrationReply{}
						reply.Data = make(map[string]string)
						reply.Seq = make(map[int64]int64)
						ok := srv.Call("ShardKV.GetMigration", &args, &reply)
						op := Op{
							OpType:         KvOp_Migration,
							MigrationReply: reply}
						if ok && reply.Err == OK {
							kv.rf.Start(op)
							break
						}
					}
					wg.Done()
				}(k)
			}

			wg.Wait()
			DPrintf("Pull shards done")
			// waitgroup done
		} else {
			kv.mu.Unlock()
		}
		time.Sleep(50 * time.Millisecond)
	}
	fmt.Println("Thread killed")
}
```
### GetMigration
```
func (kv *ShardKV) GetMigration(args *GetMigrationArgs, reply *GetMigrationReply) {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	if oldShards, versionOk := kv.oldshards[args.Num]; versionOk {
		if _, shardOk := oldShards[args.Shard]; shardOk {

			// config number -> shard number
			reply.Data = make(map[string]string)
			for k, v := range kv.oldshardsData[args.Num][args.Shard] {
				reply.Data[k] = v
			}

			reply.Seq = make(map[int64]int64)
			for k, v := range kv.oldshardsSeq[args.Num][args.Shard] {
				reply.Seq[k] = v
			}

			reply.Num = args.Num
			reply.Shard = args.Shard
			reply.Err = OK
			return
		}
	}
	reply.Err = ErrWrongGroup

}
```
### doSnapshot() go
超过最大size则生成快照
### processLog() go
取出消息队列中的消息，
msg为Valid(由rf.applyLog产生)，根据op类型进行相应处理  
applyConfig()/applyMigration()/applyGarbageCollection()/applyUserRequest()
不为valid(由installSnapshot产生)，若msg.LastIncludedIndex > kv.lastApplied，调用applySnapshot，更新lastApplied
### sendGCRequest() go
把garbageList封装成args(num, shard)组成的list，定义信号量（len(list))  
对于每个args，获取gid和groups,对于groups中的每个server,调用ShardKV.GarbageCollectionRPC，  
调用成功，若返回Ok则删除对应garbageList中shard,否则若为deleting直接break
### GarbageCollectionRPC(args, reply)
若oldConfig中没有相关num和shard的信息，返回ok(无需删除)  
封装op,放入raft日志中，若为leader返回deleting,否则返回ErrWrongLeader
### applyUserRequest(op *Op, msg *raft.ApplyMsg)
整个过程上锁
根据key值找到hashval(对应shard的id)  
对应分片不可用或者不是最新config，返回ErrWrongGroup(op.Err)  
进一步取出序列号seq，保证op序列号>seq（还未应用），根据Get/Put/Append对db进行操作，更新seq以及lastApplied,将op放入对应index的channel中
### applyGarbageCollection(op *Op, msg *raft.ApplyMsg)
如果没有op中GCNum和GCShard对应的信息，不作操作  
否则删除oldshards/oldshardsData/oldshardsSeq中对应GCNum中GCShard对应的数据
最后更新lastApplied
### applyMigration(op *Op, msg *raft.ApplyMsg)
op中num与olcConfig的相同，  
将op中shard从requiredShards删除，availableShards中变为True,db中对应Shardf更改为op中Data  
对于op中每个seq,取最大值
```
	for k, v := range op.MigrationReply.Seq {
		timeStamp, ok := kv.clients[op.MigrationReply.Shard][k]
		if ok && timeStamp > v {
			kv.clients[op.MigrationReply.Shard][k] = timeStamp
		} else {
			kv.clients[op.MigrationReply.Shard][k] = v
		}
	}
```
更新lastApplied
garbageList[Num][shard]设为True，表示需要回收
### applyConfig(op *Op, msg *raft.ApplyMsg)
整个过程上锁
当出现新的config且requiredShards为空时  
如果是第一个config,更新oldConfig和newConfig，若kv.gid和newConfig中shard对应的id相等，设置其availableShards为True  
否则，更新更新oldConfig和newConfig，若kv.gid和newConfig中shard对应的id相等（在同一组中）  
如果不可用，设置requiredShards为true,否则另外用一个availableShards来记录，设置为true，删除kv.availableShards中对应记录  
遍历availableShards（不在同一个组，不再需要），重新初始化olcConfig中对应shard中oldshards(true)/oldshardsData/oldshardSeq
对于olcConfig中每个shard, 更新kv.oldshardsData/oldshardsSeq，清楚db中相关数据,更新kv.availableShards
更新lastApplied
