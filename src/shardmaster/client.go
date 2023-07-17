package shardmaster

//
// Shardmaster clerk.
//

import "../labrpc"
import "time"
import "crypto/rand"
import "math/big"
import "sync"

type Clerk struct {
	servers []*labrpc.ClientEnd
	// Your data here.
	lastLeader int
	seqNumber  int64
	id         int64
	mu         sync.Mutex
}

func nrand() int64 {
	max := big.NewInt(int64(1) << 62)
	bigx, _ := rand.Int(rand.Reader, max)
	x := bigx.Int64()
	return x
}

func MakeClerk(servers []*labrpc.ClientEnd) *Clerk {
	ck := new(Clerk)
	ck.servers = servers
	ck.lastLeader = -1
	ck.seqNumber = 0
	ck.id = time.Now().UnixNano()
	// Your code here.
	return ck
}

//The Query RPC's argument is a configuration number.
//The shardmaster replies with the configuration that has that number.
//If the number is -1 or bigger than the biggest known configuration number,
//the shardmaster should reply with the latest configuration.
func (ck *Clerk) Query(num int) Config {
	args := &QueryArgs{}
	// Your code here.
	ck.mu.Lock()
	ck.seqNumber += 1
	args.Num = num
	args.SeqNum = ck.seqNumber
	args.Id = ck.id
	ck.mu.Unlock()
	for {
		// try each known server.
		for _, srv := range ck.servers {
			var reply QueryReply
			ok := srv.Call("ShardMaster.Query", args, &reply)
			if ok && reply.WrongLeader == false {
				return reply.Config
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
}

//The Join RPC is used by an administrator to add new replica groups
//Its argument is a set of mappings from unique, non-zero replica group identifiers (GIDs) to lists of server names.
//The shardmaster should react by creating a new configuration that includes the new replica groups.
//The new configuration should divide the shards as evenly as possible among the full set of groups,
//and should move as few shards as possible to achieve that goal.
func (ck *Clerk) Join(servers map[int][]string) {
	args := &JoinArgs{}
	// Your code here.
	ck.mu.Lock()
	ck.seqNumber += 1
	args.Servers = servers
	args.SeqNum = ck.seqNumber
	args.Id = ck.id
	ck.mu.Unlock()

	for {
		// try each known server.
		for _, srv := range ck.servers {
			var reply JoinReply
			ok := srv.Call("ShardMaster.Join", args, &reply)
			if ok && reply.WrongLeader == false {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
}

//The Leave RPC's argument is a list of GIDs of previously joined groups.
//The shardmaster should create a new configuration that does not include those groups,
//and that assigns those groups' shards to the remaining groups
//The new configuration should divide the shards as evenly as possible among the groups,
//and should move as few shards as possible to achieve that goal.
func (ck *Clerk) Leave(gids []int) {
	args := &LeaveArgs{}
	// Your code here.
	ck.mu.Lock()
	ck.seqNumber += 1
	args.GIDs = gids
	args.SeqNum = ck.seqNumber
	args.Id = ck.id
	ck.mu.Unlock()

	for {
		// try each known server.
		for _, srv := range ck.servers {
			var reply LeaveReply
			ok := srv.Call("ShardMaster.Leave", args, &reply)
			if ok && reply.WrongLeader == false {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
}

//The Move RPC's arguments are a shard number and a GID.
//The shardmaster should create a new configuration in which the shard is assigned to the group.
//The purpose of Move is to allow us to test your software.
//A Join or Leave following a Move will likely un-do the Move, since Join and Leave re-balance.
func (ck *Clerk) Move(shard int, gid int) {
	args := &MoveArgs{}
	// Your code here.
	ck.mu.Lock()
	ck.seqNumber += 1
	args.Shard = shard
	args.GID = gid
	args.SeqNum = ck.seqNumber
	args.Id = ck.id
	ck.mu.Unlock()

	for {
		// try each known server.
		for _, srv := range ck.servers {
			var reply MoveReply
			ok := srv.Call("ShardMaster.Move", args, &reply)
			if ok && reply.WrongLeader == false {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
}
