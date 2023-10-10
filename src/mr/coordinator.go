package mr

import "log"
import "net"
import "os"
import "net/rpc"
import "net/http"

import "fmt"
import "time"
import "sync"
import "encoding/json"

const (
	timeoutDuration = 200 * time.Millisecond
)

type Coordinator struct {
	// Your definitions here.
	tasks []Task
	rtasks []ReduceTask
	machines []Machine
	nReduce int
	mutex sync.Mutex
}

var MachinesNumber int

// Your code here -- RPC handlers for the worker to call.

func (c *Coordinator) Register(args *Args, reply *Reply) error {

	// 加锁保护共享资源
	c.mutex.Lock()
	NewMachine := Machine{
		id: MachinesNumber,
		status: "busy",
		Lastime: time.Now(),
		IsAlive: true,
	}
	c.machines = append(c.machines, NewMachine)
	MachinesNumber++
	c.mutex.Unlock()


	reply.Res = NewMachine.id
	reply.Status = NewMachine.status
	reply.NReduce = c.nReduce

	return nil
}

func (c *Coordinator) ReceiveHeartbeat(args *HeartbeatMessage, reply *Reply) error {
	
	c.mutex.Lock()
	c.machines[args.WorkerID].Lastime = args.Timestamp
	c.mutex.Unlock()
	return nil
}

func (c *Coordinator) AssignTask(args *WorkerInfo, reply *TaskInfo) error{
	// fmt.Printf("AssignTask called\n")
	var workers []int
	if(!c.MapDone(&workers)) {
		c.mutex.Lock()
		for i := 0; i < len(c.tasks); i++ {
			if c.tasks[i].status == "processing" &&	c.machines[c.tasks[i].AssignedWorker].IsAlive { continue }
			if c.tasks[i].status == "completed" && c.machines[c.tasks[i].AssignedWorker].IsAlive	{continue}
		
			c.tasks[i].status = "processing"
			c.tasks[i].AssignedWorker = args.WorkerID

			reply.TaskID = c.tasks[i].id
			reply.Class = c.tasks[i].class
			reply.Key = c.tasks[i].key
			break
		}
		c.mutex.Unlock()
	} else if(!c.ReduceDone()){
		c.mutex.Lock()

		for i := 0; i < len(c.rtasks); i++ {
			if c.rtasks[i].status == "completed" { continue }
			if c.rtasks[i].status == "processing" && c.machines[c.rtasks[i].AssignedWorker].IsAlive { continue }
			
			c.rtasks[i].status = "processing"
			c.rtasks[i].AssignedWorker = args.WorkerID
			// fmt.Printf("rtask: %d, worker %d \n", i, c.rtasks[i].AssignedWorker)

			reply.TaskID = c.rtasks[i].id
			reply.Class = "reduce"
			reply.Workers, _ = json.Marshal(workers)
			break;
		}
		
		c.mutex.Unlock()
	}
	return nil
}

func (c *Coordinator) MapDone(workers *[]int) bool{
	c.mutex.Lock()
	defer c.mutex.Unlock()
	mp := make(map[int] struct{})

	for _, task := range c.tasks {
		if task.status == "completed" && c.machines[task.AssignedWorker].IsAlive {
			mp[task.AssignedWorker] = struct{}{}
		}	else {
			return false;
		}
	}
	
	for k, _ := range mp {
		*workers = append(*workers, k);
	}
	fmt.Println("MapDone")
	return true
}

func (c *Coordinator) ReduceDone() bool{
	c.mutex.Lock()
	defer c.mutex.Unlock()

	for _, rtask := range c.rtasks {
		if rtask.status != "completed" { return false }
	}
	fmt.Println("ReduceDone")
	return true
}

func (c *Coordinator) CompleteTask(args *WorkerCompleteTask, reply *TaskInfo) error{

	taskID := args.TaskID
	taskClass := args.Class
	workerID := args.WorkerID

	c.mutex.Lock()
	if(taskClass == "map") {
		c.tasks[taskID].status = "completed"
		c.tasks[taskID].AssignedWorker = workerID
	} else if(taskClass == "reduce") {
		c.rtasks[taskID].status = "completed"
		c.rtasks[taskID].AssignedWorker = workerID
		// fmt.Printf("worker %d complete reduce %d\n", workerID, taskID)
	}

	c.mutex.Unlock()

	return nil
}

//
// start a thread that listens for RPCs from worker.go
//
func (c *Coordinator) server() {
	rpc.Register(c)
	rpc.HandleHTTP()
	//l, e := net.Listen("tcp", ":1234")
	sockname := coordinatorSock()
	os.Remove(sockname)
	l, e := net.Listen("unix", sockname)
	if e != nil {
		log.Fatal("listen error:", e)
	}
	go http.Serve(l, nil)
}

//
// main/mrcoordinator.go calls Done() periodically to find out
// if the entire job has finished.
//
func (c *Coordinator) Done() bool {
	ret := false

	// Your code here.
	if c.ReduceDone() {
		ret = true;
	}
	return ret
}



func (c *Coordinator) CheckAliveMachine() { 
	for {
		CurrentTimestamp := time.Now()

		c.mutex.Lock()
		for i := 0; i < len(c.machines); i++  {
			if CurrentTimestamp.Sub(c.machines[i].Lastime) > timeoutDuration && c.machines[i].IsAlive {
				// fmt.Printf("timegap: %v\n", time.Now())
				fmt.Printf("worker %d crashed at %v\n", i, time.Now())
				c.machines[i].IsAlive = false
			}
		}
		c.mutex.Unlock()

		time.Sleep(500 * time.Millisecond)
	}
}
//
// create a Coordinator.
// main/mrcoordinator.go calls this function.
// nReduce is the number of reduce tasks to use.
//
func MakeCoordinator(files []string, nReduce int) *Coordinator {
	c := Coordinator{}

	// Your code here.
	c.nReduce = nReduce
	cnt := 0
	for _, filename := range os.Args[1:] {
		task := Task{
			id: cnt,
			key: filename,
			class: "map",
			status: "idle",
		}
		cnt++
		c.tasks = append(c.tasks, task)
	}

	for i := 0; i < nReduce; i++ {
		rtask := ReduceTask {
			id: i,
			class: "reduce",
			status: "idle",
		}
		c.rtasks = append(c.rtasks, rtask)
	}
	

	fmt.Println("new coordinator\n\n")
	go c.CheckAliveMachine()
	c.server()
	return &c
}
