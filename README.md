## Preview
该项目实现了MIT 6.824(2020)中的四个lab，包括一些Chanllenge的部分，其中服务器之间通过RPC进行通信，lab2-lab4是一个层层递进的关系
## lab1 MapReduce
有多台服务器，其中一台作为server负责任务的分发以及失效worker的处理，其余都作为worker来调用Map和Reduce函数处理读写文件。  
注意：  
如果worker没有在合理的时间内完成任务(10s)，master应该将任务交给另一个worker来处理；  
worker应该将Map输出的intermediate放在当前目录下的文件中，稍后worker可以将其作为Reduce任务的输入读取

## lab2 Raft
raft是一个分布式系统的一致性协议，同时还有一定程度的容错  
复制服务通过在多个复制服务器上存储其状态(即数据)的完整副本来实现容错。复制允许服务继续运行，即使它的一些服务器遇到故障(崩溃或中断或不稳定的网络)。  
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
server中重要方法
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
在lab2和lab3的基础上继续构建支持分片的KV存储系统，分片是键/值对的子集;例如，所有以“a”开头的键可能是一个分片，所有以“b”开头的键可能是另一个分片。  
分片的优势在于提高性能：每个复制组只处理几个分片的put和get操作，并且这些组并行操作，组与组之间互不干扰;因此，总系统吞吐量(单位时间内的输入和获取)与组的数量成比例地增加。  
该系统主要由两部分组成： 
* 一系列复制组
每个复制组负责一组分片，由几个服务器组成，这些服务器使用Raft来复制组间的分片。
* shard master
决定哪个复制组应该服务于每个shard;这些信息称为配置。配置随着时间的推移而变化。客户端通过查询分片主服务器来查找对应密钥的复制组，而复制组通过查询分片主服务器来查找需要服务的分片。整个系统只有一个shardMaster，使用Raft作为容错服务实现。
### Shard Master主要RPC
#### Join(servers map[int][] string)
参数是一组映射，从gid到服务器名称列表，功能是创建一个包含新复制组的新配置来做出反应，新的配置应该在组集合中尽可能均匀地划分分片（尽可能少的移动）
#### Query(num int)
输入一个configuration num, shard master返回对应的配置config, 如果num为-1或者大于已知最新配置的num，返回最新的配置
#### Leave(gid []int)
参数是先前加入的gid列表，shard master会将这些gid从配置组中删除，并将这些组中分片分给其他组（保证尽可能均匀，且移动较少）
#### Move(shard int, gid int)
参数是一个分片号和一个gid, shard master将该分片移到该组
### server中主要函数
#### ShardKV结构
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
#### Op结构，会放入raft的日志中
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
#### rebalance函数，将config重新均匀分配
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
