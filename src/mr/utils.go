package mr

import "time"

type Task struct {
	id int 
	key string
	class string
	status string  // processing, finished
	AssignedWorker int
}

type ReduceTask struct {
	id int 
	key string
	class string
	status string  // processing, finished
	AssignedWorker int
}

type Machine struct {
	id int 
	class string
	status string
	Lastime  time.Time
	IsAlive bool
	CompleteTasks []Task
}