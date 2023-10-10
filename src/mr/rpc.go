package mr

//
// RPC definitions.
//
// remember to capitalize all names.
//

import "os"
import "strconv"
import "time"
//
// example to show how to declare the arguments
// and reply for an RPC.
//

type ExampleArgs struct {
	X int
}

type ExampleReply struct {
	Y int
}

// Add your RPC definitions here.
type Args struct {
	X int
}

type Reply struct {
	Res int
	Status string
	NReduce int
}

type HeartbeatMessage struct {
    WorkerID   int
    Timestamp  time.Time
}


type WorkerInfo struct {
	WorkerID   int
}

type WorkerCompleteTask struct {
	WorkerID   int
	TaskID   int
	Class string
}

type TaskInfo struct {
	TaskID   int
	Class string
	Key string
	Workers []byte
}
// Cook up a unique-ish UNIX-domain socket name
// in /var/tmp, for the coordinator.
// Can't use the current directory since
// Athena AFS doesn't support UNIX-domain sockets.
func coordinatorSock() string {
	s := "/var/tmp/824-mr-"
	s += strconv.Itoa(os.Getuid())
	return s
}
