# gotaskrunner 
Simple library for parallel task running and scheduling written in go 

library is built around three types:
- Client is the task scheduler
- TaskWrapper wrapper for the task interface
- Task interface that user implements

## Getting started
Create a runner
```
// Second value is polling of the scheduling
runner := NewRunner(context.TODO(), time.Second) 

max_goroutines := 1
runner.Run(max_goroutines)
```

Write a task that implements minimum interface
```
type sampleTask struct{}
func (s *sampleTask) Run() error {
    //Do something here
    return nil
}
```

Link task to the current runner 
```
wrapper := runner.SetTask(&sampleTask{})
```

Next set plan for the wrapper or run in planless
```
wrapper.PlanForExecution()
```
Plan can be set with a string or PlanBuilder

Plan is a string that is separated by ':',
the first letter is a mode, empty plan means immideate execution and no repeats
- r - repeat every duration
- l - limit how many times job is done, -1 for infinite
- o - offset duration
- e - except Exceptdate separated by commas
- s - how much to shift time of execution when ExceptDate encountered, if not supplied it is shifted by 'r'

```
wrapper.SetPlanWithString(fmt.Sprintf("r%v:l2", time.Millisecond)).PlanForExecution()
//or with builder
builder := NewPlanBuilder().SetRepeat(time.Millisecond).SetLimit(2)
wrapper.SetPlanWithBuilder(builder).PlanForExecution()

// every wrapper can be awaited
wrapper.Wait()
```

TaskRunner has several states that can be awaited with Wait() function
```
runner.Wait(TASKS_FINISHED)
```
Right now these states are implemented:


	TASKS_FINISHED     // Nothing is executing and nothing is planned
	NO_TASKS_ACTIVE    // something is planned but nothing is executing
	LAST_TASKS_ACTIVE  // nothing is planned but something is executing
	TASKS_ACTIVE       // tasks are planned and executing

