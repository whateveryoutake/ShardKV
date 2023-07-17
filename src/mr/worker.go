package mr

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io/ioutil"
	"log"
	"net/rpc"
	"os"
	"path/filepath"
	"sort"
	"time"
)

//
// Map functions return a slice of KeyValue.
//
type KeyValue struct {
	Key   string
	Value string
}

//
// use ihash(key) % NReduce to choose the reduce
// task number for each KeyValue emitted by Map.
// 生成哈希码
func ihash(key string) int {
	h := fnv.New32a()
	h.Write([]byte(key))
	return int(h.Sum32() & 0x7fffffff)
}

type ByKey []KeyValue

func (a ByKey) Len() int           { return len(a) }              //长度
func (a ByKey) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }    //交换
func (a ByKey) Less(i, j int) bool { return a[i].Key < a[j].Key } //比较Key的大小

//
// main/mrworker.go calls this function.
//
func Worker(mapf func(string, string) []KeyValue,
	reducef func(string, []string) string) {

	// Your worker implementation here.
	// 启动Worker
	for {
		// 从Master获取任务
		task := getTask()
		// 根据任务状态执行
		switch task.TaskState {
		case Map:
			mapper(&task, mapf)
		case Reduce:
			reducer(&task, reducef)
		case Wait:
			time.Sleep(5 * time.Second)
		case Exit:
			return
		}
	}
	// uncomment to send the Example RPC to the master.
	// CallExample()

}

func getTask() Task {
	args := ExampleArgs{}
	reply := Task{}
	// 执行RPC任务
	call("Master.AssignTask", &args, &reply)
	return reply
}

func reducer(task *Task, reducef func(string, []string) string) {
	// 读取本地文件中的结果
	intermediate := *readFromLocalFile(task.Intermediates)
	// 排序
	sort.Sort(ByKey(intermediate))

	dir, _ := os.Getwd()
	tempFile, err := ioutil.TempFile(dir, "mr-tmp-*")
	if err != nil {
		log.Fatal("Fail to create temp file", err)
	}
	// 相同Key的文件放到一起
	i := 0
	for i < len(intermediate) {
		j := i + 1
		for j < len(intermediate) && intermediate[j].Key == intermediate[i].Key {
			j++
		}
		values := []string{}
		for k := i; k < j; k++ {
			values = append(values, intermediate[k].Value)
		}
		output := reducef(intermediate[i].Key, values)
		fmt.Fprintf(tempFile, "%v %v\n", intermediate[i].Key, output)
		i = j
	}
	// 关闭文件
	tempFile.Close()
	// 格式化，重命名文件
	oname := fmt.Sprintf("mr-out-%d", task.TaskNumber)
	os.Rename(tempFile.Name(), oname)
	// 更新输出
	task.Output = oname
	TaskCompleted(task)
}

func mapper(task *Task, mapf func(string, string) []KeyValue) {
	// 从文件名读取content
	content, err := ioutil.ReadFile(task.Input)
	if err != nil {
		log.Fatal("fail to read file:"+task.Input, err)
	}
	// 将content交给mapf，缓存结果
	intermediates := mapf(task.Input, string(content))

	// 缓存后的结果会写到本地磁盘，并切成R份
	// 切分方式是根据key做hash

	// 创建缓存buffer
	buffer := make([][]KeyValue, task.NReducer)
	for _, intermediate := range intermediates {
		// 根据 hash(key) % NReducer 得到slot，决定分配到哪个reduce任务
		slot := ihash(intermediate.Key) % task.NReducer
		buffer[slot] = append(buffer[slot], intermediate)
	}
	// 最终输出文件，有NReducer个任务得到
	mapOutput := make([]string, 0)
	for i := 0; i < task.NReducer; i++ {
		mapOutput = append(mapOutput, writeToLocalFile(task.TaskNumber, i, &buffer[i]))
	}
	// 存储为Intermediates，方便后续Reduce任务收集
	task.Intermediates = mapOutput
	// 任务完成
	TaskCompleted(task)
}

func TaskCompleted(task *Task) {
	reply := ExampleReply{}
	// 调用RPC通知Master
	call("Master.TaskCompleted", task, &reply)
}

// 写入本地文件
func writeToLocalFile(x int, y int, kvs *[]KeyValue) string {
	// 获取当前目录
	dir, _ := os.Getwd()
	tempFile, err := ioutil.TempFile(dir, "mr-tmp-*")
	if err != nil {
		log.Fatal("Failed to create temp file", err)
	}
	enc := json.NewEncoder(tempFile)
	// 对于每一个KV键值对,写入文件
	for _, kv := range *kvs {
		if err := enc.Encode(&kv); err != nil {
			log.Fatal("Failed to write kv pair", err)
		}
	}
	// 文件关闭
	tempFile.Close()
	// 格式化字符串
	outputName := fmt.Sprintf("mr-%d-%d", x, y)
	// 重命名文件
	os.Rename(tempFile.Name(), outputName)
	return filepath.Join(dir, outputName)
}

// 从本地文件读取KV键值对
func readFromLocalFile(files []string) *[]KeyValue {
	kva := []KeyValue{}
	for _, filepath := range files {
		file, err := os.Open(filepath)
		if err != nil {
			log.Fatal("Failed to open file"+filepath, err)
		}
		dec := json.NewDecoder(file)
		for {
			var kv KeyValue
			if err := dec.Decode(&kv); err != nil {
				break
			}
			kva = append(kva, kv)
		}
		file.Close()
	}
	return &kva
}

//
// example function to show how to make an RPC call to the master.
//
// the RPC argument and reply types are defined in rpc.go.
//
func CallExample() {

	// declare an argument structure.
	args := ExampleArgs{}

	// fill in the argument(s).
	args.X = 99

	// declare a reply structure.
	reply := ExampleReply{}

	// send the RPC request, wait for the reply.
	call("Master.Example", &args, &reply)

	// reply.Y should be 100.
	fmt.Printf("reply.Y %v\n", reply.Y)
}

//
// send an RPC request to the master, wait for the response.
// usually returns true.
// returns false if something goes wrong.
//
func call(rpcname string, args interface{}, reply interface{}) bool {
	// c, err := rpc.DialHTTP("tcp", "127.0.0.1"+":1234")
	sockname := masterSock()
	c, err := rpc.DialHTTP("unix", sockname)
	if err != nil {
		log.Fatal("dialing:", err)
	}
	defer c.Close()

	err = c.Call(rpcname, args, reply)
	if err == nil {
		return true
	}

	fmt.Println(err)
	return false
}
