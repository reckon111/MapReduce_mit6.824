package mr

import "fmt"
import "log"
import "net/rpc"
import "hash/fnv"
import "time"

import "os"
import "io/ioutil"
import "encoding/json"
import "sort"

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
//
func ihash(key string) int {
	h := fnv.New32a()
	h.Write([]byte(key))
	return int(h.Sum32() & 0x7fffffff)
}


// for sorting by key.
type ByKey []KeyValue

// for sorting by key.
func (a ByKey) Len() int           { return len(a) }
func (a ByKey) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByKey) Less(i, j int) bool { return a[i].Key < a[j].Key }

// worker 的状态信息

var info = Machine{
	id: -1,
}
var WorkerTask TaskInfo		// 执行任务的信息
var intermediate []KeyValue // 缓存中间键值对
var tempFileName string  	// 临时文件名
var nReduce int 			// 分区数

//
// main/mrworker.go calls this function.
//
func Worker(mapf func(string, string) []KeyValue,
	reducef func(string, []string) string) {

	// Your worker implementation here.
	for {
		if info.id == -1 {
			CallRegister()
			go sendHeartbeat()
		} else if info.status == "idle" {
			CallAssignTask()
		} else if info.status == "busy" {
			// fmt.Println(info.status)
			if WorkerTask.Class == "map" {
				processMTask(mapf)
				CallCompleteTask()
			} else if WorkerTask.Class == "reduce" {
				processRTask(reducef)
				CallCompleteTask()
			}
			
			info.status = "idle"
		}
	}
}


func CallRegister() {

	// declare an argument structure.
	args := Args{}

	// fill in the argument(s).
	args.X = 99

	// declare a reply structure.
	reply := Reply{}

	// send the RPC request, wait for the reply.
	call("Coordinator.Register", &args, &reply)

	// reply.Y should be 100.
	info.id = reply.Res
	info.status = "idle"
	nReduce = reply.NReduce
}

func sendHeartbeat() {
	for {
		// declare an argument structure.
		args := HeartbeatMessage{}

		// fill in the argument(s).
		args.WorkerID = info.id
		args.Timestamp = time.Now()
		// declare a reply structure.
		reply := Reply{}

		// send the RPC request, wait for the reply.
		call("Coordinator.ReceiveHeartbeat", &args, &reply)
		time.Sleep(50 * time.Millisecond)
	}


}

func CallAssignTask() {
	args := WorkerInfo{}
	args.WorkerID = info.id

	reply := TaskInfo{}

	call("Coordinator.AssignTask", &args, &reply)
	if reply.Class == "" {
		time.Sleep(time.Second)
	} else {
		// fmt.Printf("class %v\n", reply)
		info.status = "busy"
		WorkerTask.TaskID = reply.TaskID
		WorkerTask.Class = reply.Class
		WorkerTask.Key = reply.Key
		json.Unmarshal(reply.Workers ,&WorkerTask.Workers)
	}
}

func processMTask(mapf func(string, string) []KeyValue) {
	// fmt.Printf("class %v\n", WorkerTask.Class)
	intermediate := []KeyValue{}
	if WorkerTask.Class == "map" {
		filename := WorkerTask.Key
		// fmt.Printf("filename: %s\n", WorkerTask.Key)
		file, err := os.Open(filename)
		if err != nil {
			log.Fatalf("cannot open %v", filename)
		}
		content, err := ioutil.ReadAll(file)
		if err != nil {
			log.Fatalf("cannot read %v", filename)
		}
		file.Close()
		kva := mapf(filename, string(content))
		intermediate = append(intermediate, kva...)
		saveIntermediate(intermediate)
	}
}

func processRTask(reducef func (string, []string) string) {
	// fmt.Println("processRTask")
	intermediate := []KeyValue{}
	filenames := make([]string, 0)
	
	// fmt.Printf("start Rtask %d in worker %d using map in %v\n",  WorkerTask.TaskID, info.id, WorkerTask.Workers)
	for _, i := range WorkerTask.Workers {
		str := fmt.Sprintf("mr-%d-%d", i, WorkerTask.TaskID)
		filenames = append(filenames, str)
	}

	for _, filename := range filenames {
		file, err := os.Open(filename)
		if err != nil {
			log.Fatalf("cannot open %v in worker %d Rtask %d", filename, info.id, WorkerTask.TaskID)
		}

		dec := json.NewDecoder(file)
		for {
		  var kv KeyValue
		  if err := dec.Decode(&kv); err != nil {
			break
		  }
		  intermediate = append(intermediate, kv)
		}

		file.Close()
	}

	sort.Sort(ByKey(intermediate))
	// fmt.Printf("doing Rtask %d in worker %d using map in %v, intermediate is %v\n",  WorkerTask.TaskID, info.id, WorkerTask.Workers, intermediate)
	oname := fmt.Sprintf("mr-out-%d", WorkerTask.TaskID)
	ofile, _ := os.Create(oname)

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

		// this is the correct format for each line of Reduce output.
		fmt.Fprintf(ofile, "%v %v\n", intermediate[i].Key, output)

		i = j
	}

	ofile.Close()
	// fmt.Printf("finished Rtask %d in worker %d using map in %v\n",  WorkerTask.TaskID, info.id, WorkerTask.Workers)
}

func CallCompleteTask() {
	args := WorkerCompleteTask{}

	// fill in the argument(s).
	args.WorkerID = info.id
	args.TaskID = WorkerTask.TaskID
	args.Class = WorkerTask.Class
	// declare a reply structure.
	reply := TaskInfo{}

	// send the RPC request, wait for the reply.
	call("Coordinator.CompleteTask", &args, &reply)
}

func saveIntermediate(intermediate []KeyValue) {
	var partitions  = make([][]KeyValue, nReduce, nReduce)

	for _, kv := range intermediate {
		partitions[ihash(kv.Key) % nReduce] = append(partitions[ihash(kv.Key) % nReduce], kv)
	}

	for i, partition := range partitions {
		filePath := fmt.Sprintf("mr-%d-%d", info.id, i);
		file, err := os.OpenFile(filePath, os.O_CREATE | os.O_WRONLY | os.O_APPEND, 0666)

		if err != nil {
			log.Fatalf("cannot open %v", filePath)
		}
		defer file.Close()
		
		enc := json.NewEncoder(file)

		for _, kv := range partition {
			if err := enc.Encode(&kv); err != nil {
				log.Fatalln("json encode ", err)
			}
		}
	} 
}

//
// send an RPC request to the coordinator, wait for the response.
// usually returns true.
// returns false if something goes wrong.
//
func call(rpcname string, args interface{}, reply interface{}) bool {
	// c, err := rpc.DialHTTP("tcp", "127.0.0.1"+":1234")
	sockname := coordinatorSock()
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

