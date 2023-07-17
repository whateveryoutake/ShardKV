package mr

import (
	"log"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"sync"
	"time"
)

// 每个task分为idle, in-progress, completed三种状态。
type MasterTaskStatus int

const (
	Idle MasterTaskStatus = iota
	InProgress
	Completed
)

// 整个MapReduce任务的四种阶段
type State int

const (
	Map State = iota
	Reduce
	Exit
	Wait
)

// 存储Map任务产生的R个中间文件的信息。
type Master struct {
	// Your definitions here.
	TaskQueue     chan *Task          // 任务队列
	TaskMeta      map[int]*MasterTask //所有任务的信息，记录任务id
	MasterPhase   State               //Master的阶段
	NReduce       int                 //reduce任务的数量
	InputFiles    []string            //输入文件
	Intermediates [][]string          //Map任务产生的R个中间文件的信息
}

// 所有任务的信息
type MasterTask struct {
	TaskStatus    MasterTaskStatus //线程状态
	StartTime     time.Time        //开始时间
	TaskReference *Task            //指向任务的指针
}

// 该执行的任务
type Task struct {
	Input         string   // 输入文件，Map
	TaskState     State    // 状态 Map/Reduce
	NReducer      int      // Reduce任务的数量 Map
	TaskNumber    int      // 任务编号 Map/Ruduce
	Intermediates []string // 中间结果 Reduce
	Output        string   // 最终输出 Reduce
}

var mu sync.Mutex

// Your code here -- RPC handlers for the worker to call.

//
// an example RPC handler.
//
// the RPC argument and reply types are defined in rpc.go.

func max(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

// master等待worker调用
func (m *Master) AssignTask(args *ExampleArgs, reply *Task) error {
	// 上锁,进程退出后解锁
	mu.Lock()
	defer mu.Unlock()
	// 如果任务队列有任务就进行分配
	if len(m.TaskQueue) > 0 {
		// 从队列中取出
		*reply = *<-m.TaskQueue
		// 记录task的启动时间，状态为执行状态
		m.TaskMeta[reply.TaskNumber].TaskStatus = InProgress
		m.TaskMeta[reply.TaskNumber].StartTime = time.Now()
	} else if m.MasterPhase == Exit {
		*reply = Task{TaskState: Exit}
	} else {
		// 没有task就让worker等待
		*reply = Task{TaskState: Wait}
	}
	return nil
}

// 如果所有的MapTask都已经完成，创建ReduceTask，转入Reduce阶段
// 如果所有的ReduceTask都已经完成，转入Exit阶段
func (m *Master) TaskCompleted(task *Task, reply *ExampleReply) error {
	mu.Lock()
	// 因为worker写在同一个文件磁盘上，对于重复的结果要丢弃
	if task.TaskState != m.MasterPhase || m.TaskMeta[task.TaskNumber].TaskStatus == Completed {
		return nil
	}
	// 更新task状态
	m.TaskMeta[task.TaskNumber].TaskStatus = Completed
	mu.Unlock()
	defer m.processTaskResult(task)
	return nil
}

func (m *Master) processTaskResult(task *Task) {
	mu.Lock()
	defer mu.Unlock()
	switch task.TaskState {
	case Map:
		// 收集intermediates信息
		for reduceTaskId, filePath := range task.Intermediates {
			m.Intermediates[reduceTaskId] = append(m.Intermediates[reduceTaskId], filePath)
		}
		// Map任务全部完成则开始创建Reduce任务
		// 另外修改Master的状态
		if m.allTaskDone() {
			m.createReduceTask()
			m.MasterPhase = Reduce
		}
	case Reduce:
		// Reduce任务全部完成后就可以退出了
		if m.allTaskDone() {
			m.MasterPhase = Exit
		}
	}
}

// 检查任务的状态是否都为已完成
func (m *Master) allTaskDone() bool {
	for _, task := range m.TaskMeta {
		if task.TaskStatus != Completed {
			return false
		}
	}
	return true
}

//
// start a thread that listens for RPCs from worker.go
//	创建一个监听RPC的线程
func (m *Master) server() {
	rpc.Register(m)
	rpc.HandleHTTP()
	//l, e := net.Listen("tcp", ":1234")
	sockname := masterSock()
	os.Remove(sockname)
	l, e := net.Listen("unix", sockname)
	if e != nil {
		log.Fatal("listen error:", e)
	}
	go http.Serve(l, nil)
}

//
// main/mrmaster.go calls Done() periodically to find out
// if the entire job has finished.
//
func (m *Master) Done() bool {
	mu.Lock()
	defer mu.Unlock()
	ret := m.MasterPhase == Exit
	// Your code here.

	return ret
}

//
// create a Master.
// main/mrmaster.go calls this function.
// nReduce is the number of reduce tasks to use.
//
func MakeMaster(files []string, nReduce int) *Master {
	m := Master{
		TaskQueue:     make(chan *Task, max(nReduce, len(files))),
		TaskMeta:      make(map[int]*MasterTask),
		MasterPhase:   Map,
		NReduce:       nReduce,
		InputFiles:    files,
		Intermediates: make([][]string, nReduce),
	}

	// Your code here.
	// 创建Map任务并切成16MB-64MB的文件
	m.createMapTask()
	// 一个程序成为master，其他成为worker
	m.server()
	// 启动一个goroutine 检查超时的并任务
	go m.catchTimeOut()
	return &m
}

// 创建Map任务
func (m *Master) createMapTask() {
	// 根据文件创建任务
	for idx, filename := range m.InputFiles {
		taskMeta := Task{
			Input:      filename,
			TaskState:  Map,
			NReducer:   m.NReduce,
			TaskNumber: idx,
		}
		// 记得在Master中加入队列
		// 并且加入TaskMeta信息，根据idx索引，状态空闲
		m.TaskQueue <- &taskMeta
		m.TaskMeta[idx] = &MasterTask{
			TaskStatus:    Idle,
			TaskReference: &taskMeta,
		}
	}
}

// 创建Reduce任务
func (m *Master) createReduceTask() {
	// Master线程的任务信息需要更新
	m.TaskMeta = make(map[int]*MasterTask)
	// 根据中间结果（NReducer个）创建任务
	for idx, files := range m.Intermediates {
		taskMeta := Task{
			TaskState:     Reduce,
			NReducer:      m.NReduce,
			TaskNumber:    idx,
			Intermediates: files,
		}
		// 记得在Master中加入队列
		// 并且加入TaskMeta信息，根据idx索引，状态空闲
		m.TaskQueue <- &taskMeta
		m.TaskMeta[idx] = &MasterTask{
			TaskStatus:    Idle,
			TaskReference: &taskMeta,
		}
	}
}
func (m *Master) catchTimeOut() {
	for {
		// 休眠5s
		time.Sleep(5 * time.Second)
		mu.Lock()
		if m.MasterPhase == Exit {
			mu.Unlock()
			return
		}
		for _, masterTask := range m.TaskMeta {
			// 如果任务正在运行并且离开始已经过去了10s
			if masterTask.TaskStatus == InProgress && time.Now().Sub(masterTask.StartTime) > 10*time.Second {
				// 加入任务队列并且更改状态为Idle
				m.TaskQueue <- masterTask.TaskReference
				masterTask.TaskStatus = Idle
			}
		}
		mu.Unlock()
	}
}
