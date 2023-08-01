该hub包含了 MIT 6.824 中的四个lab，服务器之间通过RPC进行通信
### lab1 MapReduce
有多台服务器，其中一台作为server负责任务的分发以及失效worker的处理，其余都作为worker来调用Map和Reduce函数处理读写文件。  
注意：  
如果worker没有在合理的时间内完成任务(10s)，master应该将任务交给另一个worker来处理；  
worker应该将Map输出的intermediate放在当前目录下的文件中，稍后worker可以将其作为Reduce任务的输入读取

### lab2 Raft
raft是一个分布式系统的一致性协议，同时还有一定程度的容错  
复制服务通过在多个复制服务器上存储其状态(即数据)的完整副本来实现容错。复制允许服务继续运行，即使它的一些服务器遇到故障(崩溃或中断或不稳定的网络)。  
挑战在于，失败可能导致副本保存数据的不同副本。
Raft将客户端请求组织成一个序列，称为日志，并确保所有副本服务器看到相同的日志。每个副本按日志顺序执行客户端请求，并将其应用于服务状态的本地副本。由于所有活动副本都看到相同的日志内容，因此它们都以相同的顺序执行相同的请求，从而继续具有相同的服务状态。如果服务器出现故障，但后来恢复了，raft会负责更新日志。只要至少大多数服务器还活着并且可以相互通信，Raft就会继续运行；否则 Raft将不会取得任何进展，但一旦大多数人能够再次进行通信，它就会从中断的地方重新开始。
主要RPC：  
#### StartAppendEntries(is bool) 
由leader调用，参数用于区分初始心跳和后续的AppendEntries操作；  
对每个peer(除自己外)，调用go routine并行发送请求，  
如果nextIndex[id] >= rf.LastIncludedIndex + 1，则发送AppendEntries RPC（需要添加日志条目）
否则发送InstallSnapshot RPC安装快照（更新nextIndex数组）
#### AppendEntries(args, reply) 
follower收到的关于leader调用的RPC。  
对于收到的每条Entry，如果发生冲突（index相同而term不同），则删除peer中该index及之后的日志，然后替换成Entries，否则如不在日志中则直接添加
#### RequestVote(args)
peer收到candidate的投票请求。  
如果peer也是candidate，则会比较term，若对方的term比自己大，自己转变为follower状态;  
如果对方term比自己大并且自己还没投票（或者恰好投给了对方），同时对方最后日志的信息至少跟自己一样新（term相同，index相等或更大，或term更大）;  
回复的term是两者term的较大值。
#### InstallSnapshot(args, reply)
由leader调用，如果nextIndex[id] < rf.LastIncludedIndex + 1,说明nextIndex数组没有及时更新，该方法会更新peer对应的lastIncludedIndex和lastIncludedTerm、lastApplied  
删除lastIncluedIndex之前的日志
### lab3 KVRaft
构建了一个基于Raft的kv存储系统，客户会对服务器发出请求，通过raft达成共识后会返回给用户对应的结果，主要支持的操作有Get、Put以及Append操作
